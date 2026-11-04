// Command cert-manager-porkbun-webhook is a cert-manager ACME DNS01 webhook
// solver for Porkbun.
package main

import (
	"os"

	"github.com/cert-manager/cert-manager/pkg/acme/webhook/cmd"
	"k8s.io/klog/v2"

	"github.com/AlessandroZanatta/cert-manager-porkbun-webhook/porkbun"
)

func main() {
	groupName := os.Getenv("GROUP_NAME")
	if groupName == "" {
		klog.Fatal("GROUP_NAME must be specified")
	}

	// Registers the solver with the webhook API server under groupName.
	// Issuers select it with webhook.groupName and webhook.solverName.
	cmd.RunWebhookServer(groupName, porkbun.New())
}
