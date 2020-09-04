package client

import (
	"context"
	"crypto/x509"
	"encoding/base64"
	"fmt"
	"io/ioutil"
	"mime"
	"net/http"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/twitchtv/twirp"

	pb "github.com/myopenfactory/client/api"

	"github.com/myopenfactory/client/pkg/errors"
	"github.com/myopenfactory/client/pkg/log"
	"github.com/myopenfactory/client/pkg/transport"
	"github.com/myopenfactory/client/pkg/transport/file"
)

// Config configures variables for the client
type Client struct {
	logger              *log.Logger
	Username            string // Username for the plattform
	Password            string // Password for teh plattform
	URL                 string // URL of the plattform https://myopenfactory.net/pb/ for example
	ID                  string
	RunWaitTime         time.Duration
	HealthWaitTime      time.Duration
	CertificateNotAfter time.Time
	client              pb.HTTPClient
	service             pb.ClientService

	header http.Header

	// plugins
	inbounds  map[string]transport.InboundPlugin
	outbounds map[string]transport.OutboundPlugin
}

type Option func(*Client)

// New creates client with given options
func New(logger *log.Logger, identifier string, options ...Option) (*Client, error) {
	const op errors.Op = "client.New"
	c := &Client{
		logger: logger,
		header: make(http.Header),
	}
	for _, option := range options {
		option(c)
	}
	c.ID = identifier
	if c.client == nil {
		return nil, errors.E(op, "no http client set", errors.KindUnexpected)
	}

	if err := checkParams(c); err != nil {
		return nil, errors.E(op, err)
	}

	auth := []byte(c.Username + ":" + c.Password)
	c.header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString(auth))

	c.service = pb.NewClientServiceProtobufClient(c.URL, c.client)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	requestContext, err := twirp.WithHTTPRequestHeaders(ctx, c.header)
	if err != nil {
		return nil, errors.E(op, fmt.Errorf("failed to set authorization context: %w", err))
	}

	configs, err := c.service.ListConfigs(requestContext, &pb.Empty{})
	if err != nil {
		return nil, errors.E(op, fmt.Errorf("failed to retrieve configs: %w", err))
	}

	c.inbounds = make(map[string]transport.InboundPlugin)
	c.outbounds = make(map[string]transport.OutboundPlugin)
	for _, pc := range configs.Inbound {
		switch pc.Type {
		case "FILE":
			c.inbounds[pc.ProcessId], err = file.NewInboundPlugin(c.logger, pc.Parameter)
			if err != nil {
				return nil, errors.E(op, fmt.Errorf("failed to load plugin: processid: %v: %w", pc.ProcessId, err))
			}
		}
	}
	for _, pc := range configs.Outbound {
		switch pc.Type {
		case "FILE":
			c.outbounds[pc.ProcessId], err = file.NewOutboundPlugin(c.logger, pc.ProcessId, c.service.AddMessage, c.service.AddAttachment, pc.Parameter)
			if err != nil {
				return nil, errors.E(op, fmt.Errorf("failed to load plugin: processid: %v: %w", pc.ProcessId, err))
			}
		}
	}

	return c, nil
}

func WithUsername(username string) Option {
	return func(c *Client) {
		c.Username = strings.TrimSpace(username)
	}
}

func WithPassword(password string) Option {
	return func(c *Client) {
		c.Password = strings.TrimSpace(password)
	}
}

func WithURL(url string) Option {
	return func(c *Client) {
		c.URL = strings.TrimSpace(url)
	}
}

func WithRunWaitTime(duration time.Duration) Option {
	return func(c *Client) {
		c.RunWaitTime = duration
	}
}

func WithHealthWaitTime(duration time.Duration) Option {
	return func(c *Client) {
		c.HealthWaitTime = duration
	}
}

func WithClient(client pb.HTTPClient) Option {
	return func(c *Client) {
		c.client = client
	}
}

func WithProxy(proxy string) Option {
	return func(c *Client) {
		os.Setenv("HTTP_PROXY", proxy)
	}
}

func (c *Client) Health(ctx context.Context) error {
	const op errors.Op = "healthClient.Run"

	requestContext, err := twirp.WithHTTPRequestHeaders(ctx, c.header)
	if err != nil {
		return errors.E(op, fmt.Errorf("failed to set authorization context: %w", err))
	}

	certs := c.client.(*http.Client).Transport.(*http.Transport).TLSClientConfig.Certificates
	if len(certs) == 0 {
		return errors.E(op, fmt.Errorf("failed to load client certs: no certs found"))
	}

	var notAfter time.Time
	for _, certbytes := range certs[0].Certificate {
		x509Cert, err := x509.ParseCertificate(certbytes)
		if err != nil {
			c.logger.Errorf("faild to load certificate: %v", err)
			os.Exit(1)
		}
		if x509Cert.IsCA {
			continue
		}
		notAfter = x509Cert.NotAfter
	}

	c.logger.Infof("using runwaittime=%s and healthwaittime=%s", c.RunWaitTime, c.HealthWaitTime)

	ticker := time.NewTicker(c.HealthWaitTime)
	start := time.Now()
	for {
		select {
		case <-ticker.C:
			var m runtime.MemStats
			runtime.ReadMemStats(&m)
			ctx, cancel := context.WithTimeout(requestContext, 15*time.Second)
			_, err := c.service.AddHealth(ctx, &pb.HealthInfo{
				Cpu:     float64(runtime.NumCPU()),
				Ram:     float64(m.Alloc) / 1048576.0, //Megabyte
				Status:  fmt.Sprintf("Version: %s | CertValidUntil: %s", c.ID, notAfter.Format("2006-01-02")),
				Threads: uint32(runtime.NumGoroutine()),
				Uptime:  uint64(time.Since(start).Nanoseconds()),
				Os:      fmt.Sprintf("%s_%s", runtime.GOOS, runtime.GOARCH),
			})
			cancel()
			if err != nil {
				err = fmt.Errorf("error sending health information: %w", err)
				c.logger.SystemErr(errors.E(op, err))
				continue
			}
			c.logger.Infof("sent health information to remote endpoint")
		case <-ctx.Done():
			return nil
		}
	}
}

// Runs client until context is closed
func (c *Client) Run(ctx context.Context) error {
	const op errors.Op = "client.Run"

	var err error
	requestContext, err := twirp.WithHTTPRequestHeaders(ctx, c.header)
	if err != nil {
		return errors.E(op, fmt.Errorf("failed to set authorization context: %w", err))
	}

	ticker := time.NewTicker(c.RunWaitTime)
	for {
		select {
		case <-ticker.C:
			for _, plugin := range c.outbounds {
				ctx, cancel := context.WithTimeout(requestContext, 15*time.Second)
				attachments, err := plugin.ListAttachments(ctx)
				if err != nil {
					c.logger.Errorf("error while reading attachment: %v", err)
				}
				cancel()

				for _, atc := range attachments {
					ctx, cancel := context.WithTimeout(requestContext, 5*time.Second)
					if _, err := plugin.ProcessAttachment(ctx, atc); err != nil {
						c.logger.Errorf("error while processing attachment %v: %v", atc.Filename, err)
					}
					cancel()
				}

				ctx, cancel = context.WithTimeout(requestContext, 15*time.Second)
				messages, err := plugin.ListMessages(ctx)
				if err != nil {
					c.logger.Errorf("error while reading messages: %v", err)
				}
				cancel()

				for _, msg := range messages {
					ctx, cancel = context.WithTimeout(requestContext, 15*time.Second)
					if _, err := plugin.ProcessMessage(ctx, msg); err != nil {
						c.logger.Errorf("error while processing message %v: %v", msg.Id, err)
					}
					cancel()
				}
			}

			ctx, cancel := context.WithTimeout(requestContext, 15*time.Second)
			msgs, err := c.service.ListMessages(ctx, &pb.Empty{})
			cancel()
			if err != nil {
				c.logger.Infof("failed listing remote messages: %v", err)
				continue
			}
			for _, msg := range msgs.Messages {
				p, ok := c.inbounds[msg.ProcessId]
				if !ok {
					confirm, err := transport.CreateConfirm(msg.Id, msg.ProcessId, transport.StatusInternalError, "no process configured: %v", msg.ProcessId)
					if err != nil {
						c.logger.Errorf("error while creating confirm: %v", err)
						continue
					}
					ctx, cancel := context.WithTimeout(requestContext, 15*time.Second)
					_, err = c.service.ConfirmMessage(ctx, confirm)
					if err != nil {
						c.logger.Errorf("error while sending confirm: %v", err)
					}
					cancel()
					continue
				}
				for _, attachment := range msg.Attachments {
					data, filename, err := downloadAttachment(attachment)
					if err != nil {
						confirm, err := transport.CreateConfirm(msg.Id, msg.ProcessId, transport.StatusInternalError, "error while processing inbound attachment: %v", err)
						if err != nil {
							c.logger.Errorf("error while creating confirm: %v", err)
							continue
						}
						ctx, cancel := context.WithTimeout(requestContext, 15*time.Second)
						_, err = c.service.ConfirmMessage(ctx, confirm)
						if err != nil {
							c.logger.Errorf("error while sending confirm: %v", err)
						}
						cancel()
						continue
					}

					attachment.Content = &pb.Attachment_Data{
						Data: data,
					}
					attachment.Filename = strings.Replace(attachment.Filename, "%(REALFILENAME)", filename, -1)
					ctx, cancel := context.WithTimeout(requestContext, 15*time.Second)
					p.ProcessAttachment(ctx, attachment)
					cancel()
				}

				ctx, cancel := context.WithTimeout(requestContext, 15*time.Second)
				confirm, err := p.ProcessMessage(ctx, msg)
				if err != nil {
					confirm, err = transport.CreateConfirm(msg.Id, msg.ProcessId, transport.StatusInternalError, "error while processing inbound msg: %v", err)
					if err != nil {
						c.logger.Errorf("error while creating confirm: %v", err)
						continue
					}
				}
				cancel()

				ctx, cancel = context.WithTimeout(requestContext, 15*time.Second)
				_, err = c.service.ConfirmMessage(ctx, confirm)
				if err != nil {
					c.logger.Errorf("error while sending confirm for inbound msg %s: %v", msg.Id, err)
				}
				cancel()
			}
		case <-ctx.Done():
			return nil
		}
	}
}

func downloadAttachment(attachment *pb.Attachment) ([]byte, string, error) {
	const op errors.Op = "client.downloadAttachment"
	url := attachment.GetUrl()
	if url == "" {
		return nil, "", errors.E(op, fmt.Errorf("attachment url couldn't be empty"))
	}
	if attachment.Filename == "" {
		return nil, "", errors.E(op, fmt.Errorf("error no filename found for attachment %s", attachment.Filename))
	}
	resp, err := http.Get(url)
	if err != nil {
		return nil, "", errors.E(op, fmt.Errorf("error while loading attachment with url %q: %w", attachment.GetUrl(), err))
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, "", errors.E(op, fmt.Errorf("error bad response for attachment %q: %w", attachment.GetUrl(), err))
	}

	_, params, err := mime.ParseMediaType(resp.Header.Get("Content-Disposition"))
	if err != nil {
		return nil, "", errors.E(op, fmt.Errorf("error while parsing header for attachment %q: %w", attachment.GetUrl(), err))
	}

	data, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, "", errors.E(op, fmt.Errorf("error while writing response data for attachment %q: %w", attachment.GetUrl(), err))
	}
	return data, params["filename"], nil
}

func processMessage() {
}

func checkParams(c *Client) error {
	const op errors.Op = "client.checkParams"
	if c == nil {
		return errors.E(op, "client not valid", errors.KindBadRequest)
	}
	if c.ID == "" {
		return errors.E(op, "missing id", errors.KindBadRequest)
	}
	if c.Username == "" {
		return errors.E(op, "missing username", errors.KindBadRequest)
	}
	if c.Password == "" {
		return errors.E(op, "missing password", errors.KindBadRequest)
	}
	if c.URL == "" {
		return errors.E(op, "missing url", errors.KindBadRequest)
	}
	if c.client == nil {
		return errors.E(op, "missing client", errors.KindBadRequest)
	}
	return nil
}
