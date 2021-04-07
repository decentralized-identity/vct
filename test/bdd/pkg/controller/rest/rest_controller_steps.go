/*
Copyright SecureKey Technologies Inc. All Rights Reserved.

SPDX-License-Identifier: Apache-2.0
*/

package rest

import (
	"context"
	"embed"
	"encoding/base64"
	"fmt"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/cenkalti/backoff/v4"
	"github.com/cucumber/godog"
	"github.com/google/trillian/merkle/rfc6962/hasher"

	"github.com/trustbloc/vct/pkg/client/vct"
	"github.com/trustbloc/vct/pkg/controller/command"
)

//go:embed testdata/*.json
var fs embed.FS // nolint: gochecknoglobals

// Steps represents BDD test steps.
type Steps struct {
	client *http.Client
	vct    *vct.Client
	state  struct {
		GetSTHResponse *command.GetSTHResponse
		LastEntries    []command.LeafEntry
	}
}

// New creates BDD test steps instance.
func New() *Steps {
	return &Steps{client: &http.Client{Timeout: time.Minute}}
}

// RegisterSteps registers the BDD steps on the suite.
func (s *Steps) RegisterSteps(suite *godog.Suite) {
	suite.Step(`VCT agent is running on "([^"]*)"$`, s.setVCTClient)
	suite.Step(`Add verifiable credential "([^"]*)" to Log$`, s.addVC)
	suite.Step(`Retrieve latest signed tree head and check that tree_size is "([^"]*)"$`, s.getSTH)
	suite.Step(`Retrieve merkle consistency proof between signed tree heads$`, s.getSTHConsistency)
	suite.Step(`Retrieve entries from log and check that len is "([^"]*)"$`, s.getEntries)
	suite.Step(`Retrieve merkle audit proof from log by leaf hash for entry "([^"]*)"$`, s.getProofByHash)
}

func (s *Steps) setVCTClient(endpoint string) error {
	s.vct = vct.New(endpoint, vct.WithHTTPClient(s.client))

	resp, err := s.vct.GetSTH(context.Background())

	s.state.GetSTHResponse = resp

	return err // nolint: wrapcheck
}

func (s *Steps) addVC(file string) error {
	src, err := readFile(file)
	if err != nil {
		return err
	}

	_, err = s.vct.AddVC(context.Background(), src)

	return err // nolint: wrapcheck
}

func (s *Steps) getProofByHash(idx string) error {
	id, err := strconv.Atoi(idx)
	if err != nil {
		return fmt.Errorf("parse index: %w", err)
	}

	return backoff.Retry(func() error { // nolint: wrapcheck
		resp, err := s.vct.GetSTH(context.Background())
		if err != nil {
			return fmt.Errorf("get STH: %w", err)
		}

		entries, err := s.vct.GetProofByHash(
			context.Background(),
			base64.StdEncoding.EncodeToString(hasher.DefaultHasher.HashLeaf(s.state.LastEntries[id-1].LeafInput)),
			resp.TreeSize,
		)
		if err != nil {
			return fmt.Errorf("get proof by hash: %w", err)
		}

		if len(entries.AuditPath) < 1 {
			return fmt.Errorf("no audit, expected greater than zero, got %d", len(entries.AuditPath))
		}

		return nil
	}, backoff.WithMaxRetries(backoff.NewConstantBackOff(time.Second), 15))
}

func (s *Steps) getEntries(lengths string) error {
	return backoff.Retry(func() error { // nolint: wrapcheck
		resp, err := s.vct.GetSTH(context.Background())
		if err != nil {
			return fmt.Errorf("get STH: %w", err)
		}

		entries, err := s.vct.GetEntries(context.Background(), s.state.GetSTHResponse.TreeSize, resp.TreeSize)
		if err != nil {
			return fmt.Errorf("get entries: %w", err)
		}

		entriesLen := strconv.Itoa(len(entries.Entries))
		if entriesLen != lengths {
			return fmt.Errorf("no entries, expected %s, got %s", lengths, entriesLen)
		}

		s.state.LastEntries = entries.Entries

		return nil
	}, backoff.WithMaxRetries(backoff.NewConstantBackOff(time.Second), 15))
}

func (s *Steps) getSTHConsistency() error {
	return backoff.Retry(func() error { // nolint: wrapcheck
		resp, err := s.vct.GetSTH(context.Background())
		if err != nil {
			return fmt.Errorf("get STH: %w", err)
		}

		consistency, err := s.vct.GetSTHConsistency(
			context.Background(),
			s.state.GetSTHResponse.TreeSize,
			resp.TreeSize,
		)
		if err != nil {
			return fmt.Errorf("get STH consistency: %w", err)
		}

		if s.state.GetSTHResponse.TreeSize != 0 && len(consistency.Consistency) < 1 {
			return fmt.Errorf("no hash, expected greater than zero, got %d", len(consistency.Consistency))
		}

		if s.state.GetSTHResponse.TreeSize == 0 && len(consistency.Consistency) != 0 {
			return fmt.Errorf("empty hash expected, got %d", len(consistency.Consistency))
		}

		return nil
	}, backoff.WithMaxRetries(backoff.NewConstantBackOff(time.Second), 15))
}

func (s *Steps) getSTH(treeSize string) error {
	return backoff.Retry(func() error { // nolint: wrapcheck
		resp, err := s.vct.GetSTH(context.Background())
		if err != nil {
			return fmt.Errorf("get STH: %w", err)
		}

		ts := strconv.Itoa(int(resp.TreeSize - s.state.GetSTHResponse.TreeSize))
		if ts != treeSize {
			return fmt.Errorf("expected tree size %s, got %s", treeSize, ts)
		}

		return nil
	}, backoff.WithMaxRetries(backoff.NewConstantBackOff(time.Second), 15))
}

func readFile(msgFile string) ([]byte, error) {
	return fs.ReadFile(filepath.Clean(strings.Join([]string{ // nolint: wrapcheck
		"testdata", msgFile,
	}, string(filepath.Separator))))
}