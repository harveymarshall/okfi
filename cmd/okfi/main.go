// Command okfi generates and manages Google OKF (Open Knowledge Format)
// bundles from data resources such as S3, GCS, BigQuery, and Redshift.
package main

import (
	"fmt"
	"os"

	"github.com/harveymarshall/okfi/internal/cli"
)

func main() {
	if err := cli.NewRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
