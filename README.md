# CPU Burner

A simple utility to simulate cpu intensive workloads

```
Usage: cpu-burner [--burn BURN] [--duration DURATION] [--no-lock-os-thread] [--log-every LOG-EVERY] [--quiet]

Options:
  --burn BURN, -b BURN   how much cpu to burn. Can be specified in 2 different ways: as a float/integer, representing how many cores/fraction of a core. Eg 1.5 means 1 core and a half; as a percentage, indicating total system capacity percentage. Eg on a 4 cores system, 100% means all 4 cores, 50% means 2 cores and 62.5% means 2 cores and a half [default: 1]
  --duration DURATION, -d DURATION
                         for how long to run. Pass 0 to run indefinitely [default: 0]
  --no-lock-os-thread, -L
                         by default each goroutine used to consume cpu tries to lock itself to a single OS thread, which will cause load to be concentrated on fewer cpus. This allows more precise/consistent results. Setting this flag disables that behaviour, allowing cpu load to be shared across different cpus [default: false]
  --log-every LOG-EVERY, -l LOG-EVERY
                         how often to log actual cpu usage. Use 0 to disable it [default: 5s]
  --quiet, -q            run quietly, no stderr logging [default: false]
  --help, -h             display this help and exit
```