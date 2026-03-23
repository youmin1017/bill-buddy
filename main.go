package main

import (
	"bill-buddy/internal/bot"
	"bill-buddy/internal/config"
	"bill-buddy/internal/db"

	"github.com/samber/do/v2"
)

func main() {
	i := do.New()

	config.Package(i)
	db.Package(i)

	// bot related packages
	bot.Package(i)

	do.MustInvoke[*bot.App](i)

	// Block forever — bot runs in goroutine
	select {}
}
