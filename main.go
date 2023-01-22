package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"runtime/debug"
	"syscall"

	"github.com/disgoorg/disgo"
	"github.com/disgoorg/disgo/bot"
	"github.com/disgoorg/disgo/events"
	"github.com/disgoorg/disgo/gateway"
	"github.com/disgoorg/log"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/drive/v3"
	"google.golang.org/api/option"
	"google.golang.org/api/sheets/v4"
)

const (
	gscope                              = "https://www.googleapis.com/auth/drive"
	discordConfigFilepath               = "/app/config.json"
	mountSpreadsheetPermissionsFilepath = "/app/file-permissions.json"
	mountSpreadsheetColumnDataFilepath  = "/app/column-data.csv"
	googleApiConfigRelativeFilepath     = "/app/svc-creds.json"
)

var (
	gdriveSvc  *drive.Service
	gsheetsSvc *sheets.Service
	ctx        context.Context
	dbpool     *pgxpool.Pool
	botConfig  *Config
)

func onReadyHandler(event *events.Ready) {
	log.Debug("bot is ready")
}

func main() {
	defer func() {
		log.Debug(debug.Stack())
	}()
	var err error
	botConfig, err = NewConfig(discordConfigFilepath)
	if err != nil {
		log.Fatal(err)
		log.Fatal(string(debug.Stack()))
		return
	}
	log.Debug("parsed bot config file")
	log.SetLevel(log.Level(botConfig.LogLevel))
	ctx = context.Background()

	// init db pool
	dbpool, err = pgxpool.New(
		ctx,
		fmt.Sprintf(
			"postgres://%s:%s@%s:%s/%s",
			botConfig.DBUsername,
			botConfig.DBUserPassword,
			botConfig.DBIP,
			botConfig.DBPort,
			botConfig.DBName,
		),
	)
	if err != nil {
		log.Fatal(err)
		log.Fatal(string(debug.Stack()))
		return
	}
	defer dbpool.Close()
	// ensure no issues with acquiring db connections
	_, err = dbpool.Acquire(ctx)
	if err != nil {
		log.Fatal(err)
		log.Fatal(string(debug.Stack()))
		return
	}
	log.Debug("db pool initialized")

	// init google api client
	b, err := os.ReadFile(googleApiConfigRelativeFilepath)
	if err != nil {
		log.Fatal(err)
		log.Fatal(string(debug.Stack()))
		return
	}
	gconfig, err := google.JWTConfigFromJSON(b, gscope)
	if err != nil {
		log.Fatal(err)
		log.Fatal(string(debug.Stack()))
		return
	}
	gclient := gconfig.Client(ctx)
	log.Debug("google client initialized")
	gdriveSvc, err = drive.NewService(ctx, option.WithHTTPClient(gclient))
	if err != nil {
		log.Fatal(err)
		log.Fatal(string(debug.Stack()))
		return
	}
	log.Debug("google drive service initialized")
	gsheetsSvc, err = sheets.NewService(ctx, option.WithHTTPClient(gclient))
	if err != nil {
		return
	}
	log.Debug("google sheets service initialized")

	// init discord client
	client, err := disgo.New(
		botConfig.DiscordToken,
		bot.WithGatewayConfigOpts(
			gateway.WithIntents(
				gateway.IntentGuilds,
				gateway.IntentGuildMembers,
				gateway.IntentGuildMessages,
				gateway.IntentMessageContent,
			),
		),
		bot.WithEventListenerFunc(onReadyHandler),
		bot.WithEventListenerFunc(onGuildReady),
	)
	if err != nil {
		log.Fatal(err)
		log.Fatal(string(debug.Stack()))
		return
	}
	defer client.Close(context.TODO())

	if err = client.OpenGateway(context.TODO()); err != nil {
		log.Fatal(err)
		log.Fatal(string(debug.Stack()))
		return
	}
	log.Debug("bot initialized")

	s := make(chan os.Signal, 1)
	signal.Notify(s, syscall.SIGINT, syscall.SIGTERM, os.Interrupt)
	<-s
}
