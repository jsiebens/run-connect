package main

import (
	"github.com/jsiebens/run-connect/internal/core"
	"github.com/muesli/coral"
	"os"
)

func main() {
	cmd := &coral.Command{}
	cmd.AddCommand(createServerCommand())
	cmd.AddCommand(createClientCommand())

	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func createServerCommand() *coral.Command {
	command := &coral.Command{
		Use:          "server",
		Short:        "Start a tunnel server.",
		SilenceUsage: true,
	}

	command.AddCommand(createProxyCommand())
	command.AddCommand(createForwardCommand())

	return command
}

func createProxyCommand() *coral.Command {
	command := &coral.Command{
		Use:          "proxy",
		SilenceUsage: true,
	}

	var addr string

	command.Flags().StringVarP(&addr, "addr", "a", "0.0.0.0:8080", "bind to this address.")

	command.RunE = func(cmd *coral.Command, args []string) error {
		return core.StartServer(addr, "proxy", "")
	}

	return command
}

func createForwardCommand() *coral.Command {
	command := &coral.Command{
		Use:          "forward",
		SilenceUsage: true,
	}

	var addr string
	var upstream string

	command.Flags().StringVarP(&addr, "addr", "a", "0.0.0.0:8080", "bind to this address.")
	command.Flags().StringVarP(&upstream, "upstream", "u", "", "")

	command.RunE = func(cmd *coral.Command, args []string) error {
		return core.StartServer(addr, "forward", upstream)
	}

	return command
}

func createClientCommand() *coral.Command {
	command := &coral.Command{
		Use:          "client",
		Short:        "Start a tunnel client.",
		SilenceUsage: true,
	}

	var addr string
	var remote string
	var idToken string
	var serviceAccount string
	var clientId string

	command.Flags().StringVarP(&addr, "addr", "a", "127.0.0.1:8080", "bind to this address.")
	command.Flags().StringVarP(&remote, "remote", "r", "http://127.0.0.1:8080", "")
	command.Flags().StringVarP(&idToken, "id-token", "i", "", "")
	command.Flags().StringVarP(&serviceAccount, "service-account", "s", "", "")
	command.Flags().StringVarP(&clientId, "client-id", "c", "", "")

	command.RunE = func(cmd *coral.Command, args []string) error {
		return core.StartClient(cmd.Context(), addr, remote, idToken, serviceAccount, clientId)
	}

	return command
}
