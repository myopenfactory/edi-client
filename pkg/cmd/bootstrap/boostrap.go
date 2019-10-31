// +build !windows

package bootstrap

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// Command represents the bootstrap command
var Command = &cobra.Command{
	Use:   "bootstrap",
	Short: "bootstrap the client",
	Long:  "bootstrap the client.",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("not implemented for your os")
		os.Exit(1)
	},
}
