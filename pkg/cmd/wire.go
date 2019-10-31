//+build wireinject

package cmd

import (
	"fmt"

	"github.com/google/wire"
	"github.com/myopenfactory/client/pkg/client"
	"github.com/myopenfactory/client/pkg/log"
	"github.com/myopenfactory/client/pkg/version"
	"github.com/spf13/viper"
)

func InitializeClient() (*client.Client, error) {
	wire.Build(InitializeLogger, provideClientID, provideOptions, client.New)
	return &client.Client{}, nil
}

func InitializeLogger() *log.Logger {
	wire.Build(provideLogOptions, log.New)
	return &log.Logger{}
}

func provideOptions() []client.Option {
	return []client.Option{
		client.WithUsername(viper.GetString("username")),
		client.WithPassword(viper.GetString("password")),
		client.WithURL(viper.GetString("url")),
		client.WithCA(viper.GetString("cafile")),
		client.WithCert(viper.GetString("clientcert")),
		client.WithProxy(viper.GetString("proxy")),
	}
}

func provideLogOptions() []log.Option {
	opts := []log.Option{}

	logLevel := viper.GetString("log.level")
	if logLevel != "" {
		opts = append(opts, log.WithLevel(logLevel))
	}

	logSyslog := viper.GetString("log.syslog")
	if logSyslog != "" {
		opts = append(opts, log.WithSyslog(logSyslog))
	}

	eventLog := viper.GetString("log.eventlog")
	if eventLog != "" {
		opts = append(opts, log.WithEventlog(eventLog))
	}

	logMailHost := viper.GetString("log.mail.host")
	if logMailHost != "" {
		addr := fmt.Sprintf("%s:%d", logMailHost, viper.GetInt("log.mail.port"))
		logMailFrom := viper.GetString("log.mail.from")
		logMailTo := viper.GetString("log.mail.to")
		logMailUsername := viper.GetString("log.mail.username")
		logMailPassword := viper.GetString("log.mail.password")
		opts = append(opts, log.WithMail("myOpenFactory Client", addr, logMailFrom, logMailTo, logMailUsername, logMailPassword))
	}

	logFolder := viper.GetString("log.folder")
	if logFolder != "" {
		opts = append(opts, log.WithFilesystem(logFolder))
	}

	return opts
}

func provideClientID() string {
	return fmt.Sprintf("Core_%s", version.Version)
}
