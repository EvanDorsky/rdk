// Package main is a module with a built-in "counter" component model, that will simply track numbers.
// It uses the rdk:component:generic interface for simplicity.
package main

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pkg/errors"
	pb "go.viam.com/api/robot/v1"
	"go.viam.com/utils"

	"go.viam.com/rdk/components/generic"
	"go.viam.com/rdk/logging"
	"go.viam.com/rdk/module"
	"go.viam.com/rdk/resource"
	"go.viam.com/rdk/robot/client"
)

var myModel = resource.NewModel("acme", "demo", "mycounter")

func main() {
	// We first put our component's constructor in the registry, then run a custom main so
	// we can install our own parent-connection handler that captures the parent robot client
	// for periodic GetMachineStatus polling.
	resource.RegisterComponent(generic.API, myModel, resource.Registration[resource.Resource, resource.NoNativeConfig]{
		Constructor: newCounter,
	})

	utils.ContextualMainWithSIGPIPE(mainWithArgs, module.NewLoggerFromArgs(""))
}

func mainWithArgs(ctx context.Context, args []string, logger logging.Logger) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	mod, err := module.NewModuleFromArgs(ctx)
	if err != nil {
		return err
	}
	if err := mod.AddModelFromRegistry(ctx, generic.API, myModel); err != nil {
		return err
	}

	mod.RegisterParentConnectionChangeHandler(func(rc *client.RobotClient) {
		if !rc.Connected() {
			if testing.Testing() {
				cancel()
				return
			}
			logger.Info("connection to viam-server lost; attempting to reconnect")
			if err := rc.Connect(ctx); err != nil {
				logger.Info("reconnect attempt failed; shutting down module")
				cancel()
			}
		}
	})

	if err := mod.Start(ctx); err != nil {
		mod.Close(ctx)
		return err
	}
	defer mod.Close(ctx)

	go pollMachineStatus(ctx, logger, mod)

	<-ctx.Done()
	return utils.FilterOutError(ctx.Err(), context.Canceled)
}

func pollMachineStatus(ctx context.Context, logger logging.Logger, mod *module.Module) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		rc := mod.Parent()
		if rc == nil {
			logger.Info("parent connection not yet available; skipping MachineStatus poll")
			continue
		}
		mStatus, err := rc.MachineStatus(ctx)
		if err != nil {
			logger.Warnw("MachineStatus call failed", "error", err)
			continue
		}
		logger.Info("---- Module statuses -----")
		if len(mStatus.Modules) == 0 {
			logger.Info("  (no modules reported)")
		}
		for _, ms := range mStatus.Modules {
			stateName := pb.ModuleStatus_State(ms.State)
			if ms.Error != nil {
				logger.Infof("  %s\t%s\tconsecutive_failures=%d\tlast_updated=%s\terror=%v",
					ms.Name, stateName, ms.ConsecutiveFailures, ms.LastUpdated.Format(time.RFC3339), ms.Error)
			} else {
				logger.Infof("  %s\t%s\tconsecutive_failures=%d\tlast_updated=%s",
					ms.Name, stateName, ms.ConsecutiveFailures, ms.LastUpdated.Format(time.RFC3339))
			}
		}
	}
}

// newCounter is used to create a new instance of our specific model. It is called for each component in the robot's config with this model.
func newCounter(ctx context.Context,
	deps resource.Dependencies,
	conf resource.Config,
	logger logging.Logger,
) (resource.Resource, error) {
	return &counter{
		Named: conf.ResourceName().AsNamed(),
	}, nil
}

// counter is the representation of this model. It holds only a "total" count.
type counter struct {
	resource.Named
	resource.TriviallyCloseable
	total int64
}

func (c *counter) Reconfigure(ctx context.Context, deps resource.Dependencies, conf resource.Config) error {
	atomic.StoreInt64(&c.total, 0)
	return nil
}

// DoCommand is the only method of this component. It looks up the "real" command from the map it's passed.
// Because of this, any arbitrary commands can be received, and any data returned.
func (c *counter) DoCommand(ctx context.Context, req map[string]interface{}) (map[string]interface{}, error) {
	// We look for a map key called "command"
	cmd, ok := req["command"]
	if !ok {
		return nil, errors.New("missing 'command' string")
	}

	// If it's "get" we return the current total.
	if cmd == "get" {
		return map[string]interface{}{"total": atomic.LoadInt64(&c.total)}, nil
	}

	// If it's "add" we atomically add a second key "value" to the total.
	if cmd == "add" {
		_, ok := req["value"]
		if !ok {
			return nil, errors.New("value must exist")
		}
		val, ok := req["value"].(float64)
		if !ok {
			return nil, errors.New("value must be a number")
		}
		atomic.AddInt64(&c.total, int64(val))
		// We return the new total after the addition.
		return map[string]interface{}{"total": atomic.LoadInt64(&c.total)}, nil
	}
	// The command must've been something else.
	return nil, fmt.Errorf("unknown command string %s", cmd)
}
