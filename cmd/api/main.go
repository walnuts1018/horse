package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/walnuts1018/horse/internal/api"
	"github.com/walnuts1018/horse/internal/application/mailforwarding"
	bootstrapapi "github.com/walnuts1018/horse/internal/bootstrap/api"
	"github.com/walnuts1018/horse/internal/ingestion"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(logger); err != nil {
		logger.Error("API server stopped", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		return errors.New("DATABASE_URL is required")
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	app, err := bootstrapapi.NewAPI(ctx, logger)
	if err != nil {
		return err
	}
	defer app.Close()
	mailboxPoller, err := api.NewMailboxIngestor(app.Pool, app.ObjectStore, app.Queue, logger, api.MailboxConfig{
		Address: os.Getenv("MAILBOX_IMAPS_ADDRESS"), ServerName: os.Getenv("MAILBOX_IMAPS_SERVER_NAME"),
		Mailbox:  os.Getenv("MAILBOX_IMAPS_MAILBOX"),
		Username: os.Getenv("MAILBOX_IMAPS_USERNAME"), Password: os.Getenv("MAILBOX_IMAPS_PASSWORD"),
		RecipientBase: os.Getenv("MAILBOX_RECIPIENT_BASE"), PollInterval: mailboxPollInterval(),
		MaxMessageBytes: mailboxMaxMessageBytes(), MaxMessagesPerPoll: mailboxMaxMessagesPerPoll(),
	})
	if err != nil {
		return err
	}
	mailForwardingConfig := configuredMailForwarding(
		mailboxPoller != nil,
		os.Getenv("MAIL_FORWARDING_MAILBOX_DOMAIN"),
		os.Getenv("MAIL_FORWARDING_ADDRESS_LOCAL_PART_PREFIX"),
		os.Getenv("MAILBOX_RECIPIENT_BASE"),
	)
	mailboxCtx, stopMailbox := context.WithCancel(ctx)
	defer stopMailbox()
	mailboxDone := make(chan struct{})
	go func() {
		defer close(mailboxDone)
		mailboxPoller.Run(mailboxCtx)
	}()
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	httpServer := &http.Server{
		Addr: ":" + port,
		Handler: otelhttp.NewHandler(api.New(app.Pool, app.Auth, logger, api.Config{
			PublicOrigin: app.Config.PublicOrigin, FrontendOrigin: app.Config.FrontendOrigin,
			Store: app.ObjectStore, OCR: ingestion.OCR{Endpoint: app.Config.OCREndpoint}, Queue: app.Queue,
			MailForwarding: mailForwardingConfig,
		}).Handler(), "horse-api"),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       5 * time.Minute,
		WriteTimeout:      0,
		IdleTimeout:       60 * time.Second,
	}
	serverErr := make(chan error, 1)
	go func() { serverErr <- httpServer.ListenAndServe() }()
	logger.Info("API server listening", "address", httpServer.Addr)
	select {
	case err := <-serverErr:
		stopMailbox()
		<-mailboxDone
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		stopMailbox()
		<-mailboxDone
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		return httpServer.Shutdown(shutdownCtx)
	}
}

func configuredMailForwarding(pollerEnabled bool, domain, localPartPrefix, recipientBase string) mailforwarding.Config {
	if !pollerEnabled || domain == "" || !strings.HasSuffix(localPartPrefix, "+") {
		return mailforwarding.Config{}
	}
	recipientLocalPart, recipientDomain, ok := strings.Cut(recipientBase, "@")
	if !ok || recipientLocalPart != strings.TrimSuffix(localPartPrefix, "+") || !strings.EqualFold(recipientDomain, domain) {
		return mailforwarding.Config{}
	}
	return mailforwarding.Config{MailboxDomain: domain, AddressLocalPartPrefix: localPartPrefix}
}

func mailboxPollInterval() time.Duration {
	value, err := time.ParseDuration(os.Getenv("MAILBOX_POLL_INTERVAL"))
	if err != nil {
		return 0
	}
	return value
}

func mailboxMaxMessageBytes() int64 {
	value, err := strconv.ParseInt(os.Getenv("MAILBOX_MAX_MESSAGE_BYTES"), 10, 64)
	if err != nil || value < 1 {
		return 0
	}
	return value
}

func mailboxMaxMessagesPerPoll() int {
	value, err := strconv.Atoi(os.Getenv("MAILBOX_MAX_MESSAGES_PER_POLL"))
	if err != nil || value < 1 {
		return 0
	}
	return value
}
