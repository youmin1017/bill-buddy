package config

import (
	"bytes"
	_ "embed"
	"strings"

	"github.com/samber/do/v2"
	"github.com/spf13/viper"
)

type Config struct {
	// Database connection string, e.g., "postgres://user:password@localhost:5432/dbname?sslmode=disable"
	DatabaseURL string
	// Telegram bot token, e.g., "123456:ABC-DEF1234ghIkl-zyx57W2v1u123ew11"
	TelegramBotToken string
}

var Package = do.Package(
	do.Lazy(NewConfig),
)

//go:embed config.yaml
var defaultConfig []byte

func NewConfig(i do.Injector) (*Config, error) {
	c := &Config{}

	viper.SetConfigType("yaml")
	viper.AutomaticEnv()
	viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))

	if err := viper.ReadConfig(bytes.NewBuffer(defaultConfig)); err != nil {
		return nil, err
	}

	viper.SetConfigName("config")
	viper.AddConfigPath(".")
	_ = viper.MergeInConfig()

	_ = viper.BindEnv("DatabaseURL", "DATABASE_URL")
	_ = viper.BindEnv("TelegramBotToken", "TELEGRAM_BOT_TOKEN")

	if err := viper.Unmarshal(c); err != nil {
		return nil, err
	}

	return c, nil
}
