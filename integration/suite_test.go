//go:build integration

package integration_test

import (
	"testing"

	"github.com/JaimeStill/capstone-mikoshi/integration"
)

func TestMain(m *testing.M) { integration.Main(m) }
