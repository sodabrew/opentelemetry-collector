// Copyright The OpenTelemetry Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//       http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package config // import "go.opentelemetry.io/collector/config"

import (
	"errors"
	"fmt"

	"go.uber.org/zap/zapcore"
)

var (
	errMissingExporters        = errors.New("no enabled exporters specified in config")
	errMissingReceivers        = errors.New("no enabled receivers specified in config")
	errMissingServicePipelines = errors.New("service must have at least one pipeline")
)

// Config defines the configuration for the various elements of collector or agent.
type Config struct {
	Receivers
	Exporters
	Processors
	Extensions
	Service
}

var _ validatable = (*Config)(nil)

// Validate returns an error if the config is invalid.
//
// This function performs basic validation of configuration. There may be more subtle
// invalid cases that we currently don't check for but which we may want to add in
// the future (e.g. disallowing receiving and exporting on the same endpoint).
func (cfg *Config) Validate() error {
	// Currently there is no default receiver enabled.
	// The configuration must specify at least one receiver to be valid.
	if len(cfg.Receivers) == 0 {
		return errMissingReceivers
	}

	// Validate the receiver configuration.
	for recv, recvCfg := range cfg.Receivers {
		if err := recvCfg.Validate(); err != nil {
			return fmt.Errorf("receiver \"%s\" has invalid configuration: %w", recv, err)
		}
	}

	// Currently there is no default exporter enabled.
	// The configuration must specify at least one exporter to be valid.
	if len(cfg.Exporters) == 0 {
		return errMissingExporters
	}

	// Validate the exporter configuration.
	for exp, expCfg := range cfg.Exporters {
		if err := expCfg.Validate(); err != nil {
			return fmt.Errorf("exporter \"%s\" has invalid configuration: %w", exp, err)
		}
	}

	// Validate the processor configuration.
	for proc, procCfg := range cfg.Processors {
		if err := procCfg.Validate(); err != nil {
			return fmt.Errorf("processor \"%s\" has invalid configuration: %w", proc, err)
		}
	}

	// Validate the extension configuration.
	for ext, extCfg := range cfg.Extensions {
		if err := extCfg.Validate(); err != nil {
			return fmt.Errorf("extension \"%s\" has invalid configuration: %w", ext, err)
		}
	}

	return cfg.validateService()
}

func (cfg *Config) validateService() error {
	// Validate Telemetry
	if err := cfg.Service.Telemetry.validate(); err != nil {
		return err
	}

	// Check that all enabled extensions in the service are configured.
	for _, ref := range cfg.Service.Extensions {
		// Check that the name referenced in the Service extensions exists in the top-level extensions.
		if cfg.Extensions[ref] == nil {
			return fmt.Errorf("service references extension %q which does not exist", ref)
		}
	}

	// Must have at least one pipeline.
	if len(cfg.Service.Pipelines) == 0 {
		return errMissingServicePipelines
	}

	// Check that all pipelines have at least one receiver and one exporter, and they reference
	// only configured components.
	for _, pipeline := range cfg.Service.Pipelines {
		// Validate pipeline has at least one receiver.
		if len(pipeline.Receivers) == 0 {
			return fmt.Errorf("pipeline %q must have at least one receiver", pipeline.Name)
		}

		// Validate pipeline receiver name references.
		for _, ref := range pipeline.Receivers {
			// Check that the name referenced in the pipeline's receivers exists in the top-level receivers.
			if cfg.Receivers[ref] == nil {
				return fmt.Errorf("pipeline %q references receiver %q which does not exist", pipeline.Name, ref)
			}
		}

		// Validate pipeline processor name references.
		for _, ref := range pipeline.Processors {
			// Check that the name referenced in the pipeline's processors exists in the top-level processors.
			if cfg.Processors[ref] == nil {
				return fmt.Errorf("pipeline %q references processor %q which does not exist", pipeline.Name, ref)
			}
		}

		// Validate pipeline has at least one exporter.
		if len(pipeline.Exporters) == 0 {
			return fmt.Errorf("pipeline %q must have at least one exporter", pipeline.Name)
		}

		// Validate pipeline exporter name references.
		for _, ref := range pipeline.Exporters {
			// Check that the name referenced in the pipeline's Exporters exists in the top-level Exporters.
			if cfg.Exporters[ref] == nil {
				return fmt.Errorf("pipeline %q references exporter %q which does not exist", pipeline.Name, ref)
			}
		}
	}
	return nil
}

// Service defines the configurable components of the service.
type Service struct {
	Telemetry ServiceTelemetry

	// Extensions are the ordered list of extensions configured for the service.
	Extensions []ComponentID

	// Pipelines are the set of data pipelines configured for the service.
	Pipelines Pipelines
}

// ServiceTelemetry defines the configurable settings for service telemetry.
type ServiceTelemetry struct {
	Logs ServiceTelemetryLogs `mapstructure:"logs"`
}

func (srvT *ServiceTelemetry) validate() error {
	return srvT.Logs.validate()
}

// ServiceTelemetryLogs defines the configurable settings for service telemetry logs.
// This MUST be compatible with zap.Config. Cannot use directly zap.Config because
// the collector uses mapstructure and not yaml tags.
type ServiceTelemetryLogs struct {
	// Level is the minimum enabled logging level.
	Level zapcore.Level `mapstructure:"level"`

	// Development puts the logger in development mode, which changes the
	// behavior of DPanicLevel and takes stacktraces more liberally.
	Development bool `mapstructure:"development"`

	// Encoding sets the logger's encoding.
	// Valid values are "json" and "console".
	Encoding string `mapstructure:"encoding"`
}

func (srvTL *ServiceTelemetryLogs) validate() error {
	if srvTL.Encoding != "json" && srvTL.Encoding != "console" {
		return fmt.Errorf(`service telemetry logs invalid encoding: %q, valid values are "json" and "console"`, srvTL.Encoding)
	}
	return nil
}

// Type is the component type as it is used in the config.
type Type string

// validatable defines the interface for the configuration validation.
type validatable interface {
	// Validate validates the configuration and returns an error if invalid.
	Validate() error
}

// Unmarshallable defines an optional interface for custom configuration unmarshaling.
// A configuration struct can implement this interface to override the default unmarshaling.
type Unmarshallable interface {
	// Unmarshal is a function that unmarshals a config.Map into the unmarshable struct in a custom way.
	// The config.Map for this specific component may be nil or empty if no config available.
	Unmarshal(component *Map) error
}

// DataType is the data type that is supported for collection. We currently support
// collecting metrics, traces and logs, this can expand in the future.
type DataType string

// Currently supported data types. Add new data types here when new types are supported in the future.
const (
	// TracesDataType is the data type tag for traces.
	TracesDataType DataType = "traces"

	// MetricsDataType is the data type tag for metrics.
	MetricsDataType DataType = "metrics"

	// LogsDataType is the data type tag for logs.
	LogsDataType DataType = "logs"
)

// Pipeline defines a single pipeline.
type Pipeline struct {
	Name       string
	InputType  DataType
	Receivers  []ComponentID `mapstructure:"receivers"`
	Processors []ComponentID `mapstructure:"processors"`
	Exporters  []ComponentID `mapstructure:"exporters"`
}

// Pipelines is a map of names to Pipelines.
type Pipelines map[ComponentID]*Pipeline
