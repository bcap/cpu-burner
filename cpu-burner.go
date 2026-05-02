package main

import (
	"context"
	"fmt"
	"io"
	"log"
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
	Quiet          bool          `arg:"-q,--quiet" default:"false" help:"run quietly, no stderr logging"`
}

func main() {
	args := Args{}
	parser := arg.MustParse(&args)

	if args.Quiet {
		log.SetOutput(io.Discard)
	}

	cpus, err := parseBurn(args.Burn)
	if err != nil {
		parser.Fail(err.Error())
	}

	if cpus > float64(runtime.NumCPU()) {
		log.Printf("WARNING: burn value %.2f is larger than the number of available CPUs (%.2f)", cpus, float64(runtime.NumCPU()))
	}

	ctx := context.Background()
	if args.Duration > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, args.Duration)
		defer cancel()
		log.Printf("pid %d consuming %0.2f cpus for %d milliseconds", os.Getpid(), cpus, args.Duration/time.Millisecond)
	} else {
		log.Printf("pid %d consuming %0.2f cpus until the process is interrupted", os.Getpid(), cpus)
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
							// log.Printf("pid %d adjusting burn: %s -> %s / %s -> %s", os.Getpid(), runFor, newRunFor, sleepFor, newSleepFor)
							sleepFor = newSleepFor
							runFor = newRunFor
						}
						previousCPUTime = currentCPUTime
						previousWallTime = currentWallTime
					}
				}

				// listen for ctx.Done() every 100 iterations to avoid doing it too often
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
					log.Printf("pid %d cpu usage: %.3f (%+.1f%% from target)", os.Getpid(), cpuBurned, deltaPct)
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
