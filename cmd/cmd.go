package cmd

import (
	"os"

	log "github.com/sirupsen/logrus"

	_ "github.com/InazumaV/V2bX/core/imports"
	"github.com/spf13/cobra"
)

var command = &cobra.Command{
	Use: "V2bX",
}

func Run() {
	err := command.Execute()
	if err != nil {
		log.WithField("err", err).Error("Execute command failed")
		// A failed command must not exit with status 0, otherwise a unit with
		// Restart=on-failure stays inactive after a startup error.
		os.Exit(1)
	}
}
