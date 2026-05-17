package main

import "fmt"

const labHelpText = `usage: slimference lab <cert-trust|root-arm|root-disarm|enable|disable> [flags]

Advanced global routing lab. These commands may affect machine-wide
chatgpt.com routing and are never part of the default Codex-only path.

Normal Codex path:
  slimference enable
  slimference disable
  slimference codex run -- <prompt>

Global lab path:
  slimference lab cert-trust
  slimference lab root-arm --global-chatgpt-hosts
  slimference lab enable
  slimference lab disable
  slimference lab root-disarm
`

func handleLabCmd(args []string) {
	exitFn(runLabCmd(args, defaultInstallPrinter()))
}

func runLabCmd(args []string, p installPrinter) int {
	if len(args) == 0 || args[0] == "--help" || args[0] == "-h" || args[0] == "help" {
		fmt.Fprint(p.Out, labHelpText)
		return 0
	}
	switch args[0] {
	case "cert-trust":
		handleCertTrustCmd(args[1:])
		return 0
	case "root-arm":
		handleRootArmCmd(args[1:])
		return 0
	case "root-disarm":
		handleRootDisarmCmd(args[1:])
		return 0
	case "enable":
		return runLabEnableCmd(args[1:], p)
	case "disable":
		return runLabDisableCmd(args[1:], p)
	default:
		fmt.Fprintf(p.Err, "lab: unknown subcommand %q\n", args[0])
		fmt.Fprint(p.Err, "usage: slimference lab <cert-trust|root-arm|root-disarm|enable|disable>\n")
		return 2
	}
}
