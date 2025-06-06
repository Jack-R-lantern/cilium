// SPDX-License-Identifier: Apache-2.0
// Copyright Authors of Cilium

package cli

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/cilium/cilium/cilium-cli/defaults"
	"github.com/cilium/cilium/cilium-cli/encrypt"
	"github.com/cilium/cilium/cilium-cli/status"
)

func newCmdEncrypt() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "encryption",
		Short:   "Cilium encryption",
		Long:    ``,
		Aliases: []string{"encrypt"},
	}
	cmd.AddCommand(newCmdEncryptStatus())
	cmd.AddCommand(newCmdIPsecRotateKey())
	cmd.AddCommand(newCmdIPsecKeyStatus())
	cmd.AddCommand(newCmdNewIPsecKey())
	return cmd
}

func newCmdEncryptStatus() *cobra.Command {
	params := encrypt.Parameters{}
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Display encryption status",
		Long:  "This command returns encryption status from all nodes in the cluster",
		RunE: func(_ *cobra.Command, _ []string) error {
			params.CiliumNamespace = namespace
			s := encrypt.NewEncrypt(k8sClient, params)
			if err := s.PrintEncryptStatus(context.Background()); err != nil {
				fatalf("Unable to print encryption status: %s", err)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&params.AgentPodSelector, "agent-pod-selector", defaults.AgentPodSelector, "Label on cilium-agent pods to select with")
	cmd.Flags().StringVar(&params.NodeName, "node", "", "Node from which encryption status will be fetched, omit to select all nodes")
	cmd.Flags().BoolVar(&params.PerNodeDetails, "per-node-details", false, "Encryption status will be displayed for each cluster node separately")
	cmd.Flags().DurationVar(&params.WaitDuration, "wait-duration", 1*time.Minute, "Maximum time to wait for result, default 1 minute")
	cmd.Flags().StringVarP(&params.Output, "output", "o", status.OutputSummary, "Output format. One of: json, summary")
	return cmd
}

func newCmdIPsecRotateKey() *cobra.Command {
	params := encrypt.Parameters{}
	cmd := &cobra.Command{
		Use:   "rotate-key",
		Short: "Rotate IPsec key",
		Long:  "This command rotates IPsec encryption key in the cluster",
		RunE: func(_ *cobra.Command, _ []string) error {
			params.CiliumNamespace = namespace
			if err := checkParams(params); err != nil {
				fatalf("Input params are invalid: %s", err)
			}
			s := encrypt.NewEncrypt(k8sClient, params)
			if err := s.IPsecRotateKey(context.Background()); err != nil {
				fatalf("Unable to rotate IPsec key: %s", err)
			}
			return nil
		},
	}
	cmd.Flags().StringVarP(&params.IPsecKeyAuthAlgo, "auth-algo", "", "", "IPsec key authentication algorithm. One of: rfc4106-gcm-aes, cbc-aes-sha256, cbc-aes-sha512")
	cmd.Flags().DurationVar(&params.WaitDuration, "wait-duration", 1*time.Minute, "Maximum time to wait for result, default 1 minute")
	return cmd
}

func newCmdIPsecKeyStatus() *cobra.Command {
	params := encrypt.Parameters{}
	cmd := &cobra.Command{
		Use:     "key-status",
		Aliases: []string{"ks"},
		Short:   "Display IPsec key",
		Long:    "This command displays IPsec encryption key",
		RunE: func(_ *cobra.Command, _ []string) error {
			params.CiliumNamespace = namespace
			s := encrypt.NewEncrypt(k8sClient, params)
			if err := s.IPsecKeyStatus(context.Background()); err != nil {
				fatalf("Unable to display IPsec key: %s", err)
			}
			return nil
		},
	}
	cmd.Flags().DurationVar(&params.WaitDuration, "wait-duration", 1*time.Minute, "Maximum time to wait for result, default 1 minute")
	cmd.Flags().StringVarP(&params.Output, "output", "o", status.OutputSummary, "Output format. One of: json, summary")
	return cmd
}

func newCmdNewIPsecKey() *cobra.Command {
	params := encrypt.Parameters{}
	cmd := &cobra.Command{
		Use:   "create-key",
		Short: "Create IPsec key",
		Long:  "This command creates IPsec encryption key for the cluster",
		RunE: func(_ *cobra.Command, _ []string) error {
			params.CiliumNamespace = namespace
			if err := checkParams(params); err != nil {
				fatalf("Input params are invalid: %s", err)
			}
			s := encrypt.NewEncrypt(k8sClient, params)
			if err := s.IPsecNewKey(context.Background()); err != nil {
				fatalf("Unable to create IPsec key: %s", err)
			}
			return nil
		},
	}
	cmd.Flags().StringVarP(&params.IPsecKeyAuthAlgo, "auth-algo", "", "rfc4106-gcm-aes", "IPsec key authentication algorithm. One of: rfc4106-gcm-aes, cbc-aes-sha256, cbc-aes-sha512")
	cmd.Flags().DurationVar(&params.WaitDuration, "wait-duration", 1*time.Minute, "Maximum time to wait for result, default 1 minute")
	return cmd
}

func checkParams(params encrypt.Parameters) error {
	if !encrypt.IsIPsecAlgoSupported(params.IPsecKeyAuthAlgo) {
		return fmt.Errorf("auth-algo has invalid value: %s", params.IPsecKeyAuthAlgo)
	}
	return nil
}
