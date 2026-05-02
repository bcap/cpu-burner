# CPU Burner

A simple utility to simulate cpu intensive workloads

## Installation

Download a pre-built binary from the [releases page](https://github.com/bcap/cpu-burner/releases), or install via Go:

```sh
go install github.com/bcap/cpu-burner@latest
```

## Usage

```
Usage: cpu-burner [--burn BURN] [--duration DURATION] [--lock-os-thread] [--log-every LOG-EVERY] [--verbose] [--quiet]

Options:
  --burn BURN, -b BURN   how much cpu to burn. Can be specified in 2 different ways: as a float/integer, representing how many cores/fraction of a core. Eg 1.5 means 1 core and a half; as a percentage, indicating total system capacity percentage. Eg on a 4 cores system, 100% means all 4 cores, 50% means 2 cores and 62.5% means 2 cores and a half [default: 1]
  --duration DURATION, -d DURATION
                         for how long to run. Pass 0 to run indefinitely [default: 0]
  --lock-os-thread       will make each goroutine used to consume cpu lock itself to a single OS thread, which should cause load to be concentrated on fewer cpus [default: false]
  --log-every LOG-EVERY, -l LOG-EVERY
                         how often to log actual cpu usage. Use 0 to disable it [default: 10s]
  --verbose, -v          enable debug logging [default: false]
  --quiet, -q            disable all logging [default: false]
  --help, -h             display this help and exit
```

## Releasing

Releases are automated via GitHub Actions on version tags. To cut a release:

```sh
git tag v0.1.0
git push --tags
```

GoReleaser will build binaries for linux/darwin × amd64/arm64 and publish a GitHub release.
