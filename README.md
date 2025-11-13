# Prerequisites
Make sure you have Docker and Go installed.

SLRun was developed and tested on Ubuntu 25.10.

# Configuration
See `example_config.json` and example functions in `functions/`.

```json
{
  "policy": "always_hot",
  "functions": [
    {
      "name": "func1",
      "build_dir": "./functions/func1"
    },
    {
      "name": "func2",
      "build_dir": "./functions/func2"
    }
  ]
}
```

For each function, the `name` must be unique, and the directory pointed by `build_dir` must contain a Dockerfile located at its root, along with all other files required to build the function's image.

Currently supported policies are `always_hot`, `always_cold` and `cold_on_idle`.

# Execution
To use `example_config.json` and port `1337`, you may use
```
go build
./slrun --port 1337 --config ./example_config.json
```

To verify that it works, run

```
curl localhost:1337/func1
curl localhost:1337/func2
```

You should see the responses from the functions.
