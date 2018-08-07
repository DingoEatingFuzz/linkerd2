package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/linkerd/linkerd2/controller/api/public"
	healthcheckPb "github.com/linkerd/linkerd2/controller/gen/common/healthcheck"
	pb "github.com/linkerd/linkerd2/controller/gen/public"
	"github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/pkg/browser"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

// These constants are used by the `show` flag.
const (
	// showLinkerd opens the Linkerd dashboard in a web browser (default).
	showLinkerd = "linkerd"

	// showGrafana opens the Grafana dashboard in a web browser.
	showGrafana = "grafana"

	// showURL displays dashboard URLs without opening a browser.
	showURL = "url"
)

type dashboardOptions struct {
	dashboardProxyPort int
	dashboardShow      string
}

func newDashboardOptions() *dashboardOptions {
	return &dashboardOptions{
		dashboardProxyPort: 0,
		dashboardShow:      showLinkerd,
	}
}

func newCmdDashboard() *cobra.Command {
	options := newDashboardOptions()

	cmd := &cobra.Command{
		Use:   "dashboard [flags]",
		Short: "Open the Linkerd dashboard in a web browser",
		RunE: func(cmd *cobra.Command, args []string) error {
			if options.dashboardProxyPort < 0 {
				return fmt.Errorf("port must be greater than or equal to zero, was %d", options.dashboardProxyPort)
			}

			if options.dashboardShow != showLinkerd && options.dashboardShow != showGrafana && options.dashboardShow != showURL {
				return fmt.Errorf("unknown value for 'show' param, was: %s, must be one of: %s, %s, %s",
					options.dashboardShow, showLinkerd, showGrafana, showURL)
			}

			kubernetesProxy, err := k8s.NewProxy(kubeconfigPath, options.dashboardProxyPort)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Failed to initialize proxy: %s\n", err)
				os.Exit(1)
			}

			url, err := kubernetesProxy.URLFor(controlPlaneNamespace, "/services/web:http/proxy/")
			if err != nil {
				fmt.Fprintf(os.Stderr, "Failed to generate URL for dashboard: %s\n", err)
				os.Exit(1)
			}

			grafanaUrl, err := kubernetesProxy.URLFor(controlPlaneNamespace, "/services/grafana:http/proxy/")
			if err != nil {
				fmt.Fprintf(os.Stderr, "Failed to generate URL for Grafana: %s\n", err)
				os.Exit(1)
			}

			client, err := checkClusterAvailability()
			if err != nil {
				fmt.Fprintf(os.Stderr, "Cannot connect to Kubernetes: %s\n", err)
				os.Exit(1)
			}

			err = checkDashboardAvailability(client)
			if err != nil {
				log.Debugf("Error checking dashboard availability: %s", err)
				fmt.Fprintf(os.Stderr, "Linkerd is not running in the \"%s\" namespace\n", controlPlaneNamespace)
				fmt.Fprintf(os.Stderr, "Install with: linkerd install --linkerd-namespace %s | kubectl apply -f -\n", controlPlaneNamespace)
				os.Exit(1)
			}

			fmt.Printf("Linkerd dashboard available at:\n%s\n", url.String())
			fmt.Printf("Grafana dashboard available at:\n%s\n", grafanaUrl.String())

			switch options.dashboardShow {
			case showLinkerd:
				fmt.Println("Opening Linkerd dashboard in the default browser")

				err = browser.OpenURL(url.String())
				if err != nil {
					fmt.Fprintf(os.Stderr, "Failed to open Linkerd URL %s in the default browser: %s", url, err)
					os.Exit(1)
				}
			case showGrafana:
				fmt.Println("Opening Grafana dashboard in the default browser")

				err = browser.OpenURL(grafanaUrl.String())
				if err != nil {
					fmt.Fprintf(os.Stderr, "Failed to open Grafana URL %s in the default browser: %s", grafanaUrl, err)
					os.Exit(1)
				}
			case showURL:
				// no-op, we already printed the URLs
			}

			// blocks until killed
			err = kubernetesProxy.Run()
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error running proxy: %s", err)
				os.Exit(1)
			}

			return nil
		},
	}

	cmd.Args = cobra.NoArgs
	// This is identical to what `kubectl proxy --help` reports, `--port 0` indicates a random port.
	cmd.PersistentFlags().IntVarP(&options.dashboardProxyPort, "port", "p", options.dashboardProxyPort, "The port on which to run the proxy (when set to 0, a random port will be used)")
	cmd.PersistentFlags().StringVar(&options.dashboardShow, "show", options.dashboardShow, "Open a dashboard in a browser or show URLs in the CLI (one of: linkerd, grafana, url)")

	return cmd
}

func checkClusterAvailability() (client pb.ApiClient, err error) {
	if apiAddr != "" {
		client, err = public.NewInternalClient(controlPlaneNamespace, apiAddr)
	} else {
		var kubeAPI k8s.KubernetesApi
		kubeAPI, err = k8s.NewAPI(kubeconfigPath)
		if err != nil {
			return
		}

		for _, result := range kubeAPI.SelfCheck() {
			if result.Status != healthcheckPb.CheckStatus_OK {
				err = fmt.Errorf(result.FriendlyMessageToUser)
				return
			}
		}

		client, err = public.NewExternalClient(controlPlaneNamespace, kubeAPI)
	}

	return
}

func checkDashboardAvailability(client pb.ApiClient) error {
	res, err := client.SelfCheck(context.Background(), &healthcheckPb.SelfCheckRequest{})
	if err != nil {
		return err
	}

	for _, result := range res.Results {
		if result.Status != healthcheckPb.CheckStatus_OK {
			return fmt.Errorf(result.FriendlyMessageToUser)
		}
	}

	return nil
}
