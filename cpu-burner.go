package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/alexflint/go-arg"
)

type Args struct {
	Burn           string        `arg:"-b,--burn" default:"1" help:"how much cpu to burn. Can be specified in 2 different ways: as a float/integer, representing how many cores/fraction of a core. Eg 1.5 means 1 core and a half; as a percentage, indicating total system capacity percentage. Eg on a 4 cores system, 100% means all 4 cores, 50% means 2 cores and 62.5% means 2 cores and a half"`
	Duration       time.Duration `arg:"-d,--duration" default:"0" help:"for how long to run. Pass 0 to run indefinitely"`
	NoLockOSThread bool          `arg:"--lock-os-thread" default:"false" help:"will make each goroutine used to consume cpu lock itself to a single OS thread, which should cause load to be concentrated on fewer cpus"`
	LogEvery       time.Duration `arg:"-l,--log-every" default:"10s" help:"how often to log actual cpu usage. Use 0 to disable it"`
	Verbose        bool          `arg:"-v,--verbose" default:"false" help:"enable debug logging"`
	Quiet          bool          `arg:"-q,--quiet" default:"false" help:"disable all logging"`
}

func main() {
	args := Args{}
	parser := arg.MustParse(&args)

	level := slog.LevelInfo
	if args.Verbose {
		level = slog.LevelDebug
	}
	opts := &slog.HandlerOptions{Level: level}
	var handler slog.Handler
	if args.Quiet {
		handler = slog.DiscardHandler
	} else {
		handler = slog.NewTextHandler(os.Stderr, opts)
	}
	slog.SetDefault(slog.New(handler))

	cpus, err := parseBurn(args.Burn)
	if err != nil {
		parser.Fail(err.Error())
	}

	if cpus > float64(runtime.NumCPU()) {
		slog.Warn("burn value exceeds available CPUs", "burn", cpus, "cpus", runtime.NumCPU())
	}

	ctx := context.Background()
	if args.Duration > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, args.Duration)
		defer cancel()
		slog.Info("consuming cpus", "pid", os.Getpid(), "cpus", cpus, "duration_ms", args.Duration.Milliseconds())
	} else {
		slog.Info("consuming cpus until interrupted", "pid", os.Getpid(), "cpus", cpus)
	}

	burn(ctx, cpus, !args.NoLockOSThread, args.LogEvery)
}

func parseBurn(burn string) (float64, error) {
	invalidInput := fmt.Errorf("invalid burn value: %s", burn)
	cpus := float64(runtime.NumCPU())

	// float-like parsing, eg: 3.5 means 3 cores and a half
	value, err := strconv.ParseFloat(burn, 64)
	if err == nil {
		if value < 0 {
			return 0, invalidInput
		}
		return value, nil
	}

	// percentage-like parsing, eg 50% on a 4 core system means 2 cores
	if strings.LastIndex(burn, "%") != len(burn)-1 {
		return 0, invalidInput
	}
	value, err = strconv.ParseFloat(burn[:len(burn)-1], 64) // parse without the the % symbol at the end
	if err != nil || value < 0 {
		return 0, invalidInput
	}

	return value / 100.0 * cpus, nil
}

const adjustTimingsEveryXIterations = 100
const checkContextEveryXIterations = 100
const detectionFactor = 0.005 // if actual cpu usage is off by more than .5% from the target, adjust sleep and run times
const adjustmentFactor = 0.01 // when adjusting sleep and run times, adjust them by 1% (eg if sleepFor is 100ms and we need to increase it, we will increase it to 101ms)

func burn(ctx context.Context, cpus float64, lockOSThread bool, logEvery time.Duration) {
	workUnit := 1000 * time.Microsecond
	work := cpus

	wg := sync.WaitGroup{}
	for work > 0 {
		share := 1.0
		if work < 1 {
			share = work
		}
		work -= share
		wg.Add(1)
		go func(share float64) {
			defer wg.Done()
			if lockOSThread {
				runtime.LockOSThread()
				defer runtime.UnlockOSThread()
			}
			runFor := time.Duration(float64(workUnit) * share)
			sleepFor := workUnit - runFor
			var iterations int64 = 1
			var previousCPUTime int64 = cpuTime()
			previousWallTime := time.Now()
			for {
				unitStart := time.Now()
				for time.Since(unitStart) < runFor {
					// this tight loop should take 100% of a core
				}

				// In practice only one goroutine will be splitting its time between sleeping and running.
				// All others (if any) will be running all the time
				// For that reason its ok for this goroutine to use cpuTime() (which gives global cpu utilizaiton)
				// and make sleep adjustments based on that
				if sleepFor > 0 {
					time.Sleep(sleepFor)

					// Check if we need to adjust sleepFor
					if iterations%adjustTimingsEveryXIterations == 0 {
						currentCPUTime := cpuTime()
						currentWallTime := time.Now()
						actualCPUs := float64(currentCPUTime-previousCPUTime) / float64(currentWallTime.Sub(previousWallTime))
						delta := actualCPUs - cpus
						newSleepFor := sleepFor
						newRunFor := runFor
						if delta < -cpus*detectionFactor {
							newSleepFor = time.Duration(float64(sleepFor) * (1 - adjustmentFactor))
							newRunFor = time.Duration(float64(runFor) * (1 + adjustmentFactor))
						} else if delta > cpus*detectionFactor {
							newSleepFor = time.Duration(float64(sleepFor) * (1 + adjustmentFactor))
							newRunFor = time.Duration(float64(runFor) * (1 - adjustmentFactor))
						}
						if newSleepFor != sleepFor {
							slog.Debug("adjusting burn timings", "pid", os.Getpid(), "run_for", runFor, "new_run_for", newRunFor, "sleep_for", sleepFor, "new_sleep_for", newSleepFor)
							sleepFor = newSleepFor
							runFor = newRunFor
						}
						previousCPUTime = currentCPUTime
						previousWallTime = currentWallTime
					}
				}

				// listen for ctx.Done() every few iterations to avoid doing it too often
				if iterations%checkContextEveryXIterations == 0 {
					select {
					case <-ctx.Done():
						return
					default:
					}
				}

				iterations++
			}
		}(share)
	}

	if logEvery > 0 {
		wg.Add(1)
		go func() {
			defer wg.Done()

			ticker := time.NewTicker(logEvery)
			defer ticker.Stop()

			previous := cpuTime()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					current := cpuTime()
					cpuBurned := float64(current-previous) / float64(logEvery)
					deltaPct := (cpuBurned - cpus) / cpus * 100
					slog.Info("cpu usage", "pid", os.Getpid(), "cpus", fmt.Sprintf("%.3f", cpuBurned), "delta_pct", fmt.Sprintf("%+.1f%%", deltaPct))
					previous = current
				}
			}
		}()
	}

	wg.Wait()
}

func cpuTime() int64 {
	var usage syscall.Rusage
	syscall.Getrusage(syscall.RUSAGE_SELF, &usage)
	return usage.Utime.Nano()
}
