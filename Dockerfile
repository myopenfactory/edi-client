FROM alpine:latest
RUN apk add --no-cache tzdata ca-certificates
COPY myof-client /myof-client
COPY myOpenFactoryCA.crt /usr/local/share/ca-certificates/extra/myOpenFactoryCA.crt
RUN update-ca-certificates

LABEL org.opencontainers.image.source="https://github.com/myopenfactory/client"

ENTRYPOINT ["/myof-client"]