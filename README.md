# influxctl

*CLI to control InfluxDB v3 instances*

## Getting Started

`influxctl` uses a TOML configuration file to store the connection details and
other settings used to talk to InfluxDB v3 instances.

Users can pass a configuration file with the `--config` option:

```shell
./influxctl --config config.toml database list
```

or if no config option is provided look in the following platform dependent
locations:

* Linux: `$HOME/.config/influxctl/config.toml`
* macOS: `$HOME/Library/Application Support/influxctl/config.toml`
* Windows: `%APPDATA%\influxctl\config.toml`

## Configuration

### Example Configurations

See the included `config.toml` for the full list of configuration options.

### InfluxDB v3 Clustered Configuration

The following is an example Cloud Clustered configuration:

```toml
[[profile]]
    name = "default"
    product = "clustered"
    host = "mycluster.mydomain.com"
    port = "443"

    [profile.auth.oauth2]
        client_id = "1cc2cfec-faf9-4751-a63c-06a1c4......"
        scopes = ["1cc2cfec-faf9-4751-a63c-06a1c4....../.default"]
        device_url = "https://login.microsoftonline.com/9e2f24dd-afa6-4816-aaaf-5e8d70....../oauth2/v2.0/devicecode"
        token_url = "https://login.microsoftonline.com/9e2f24dd-afa6-4816-aaaf-5e8d70....../oauth2/v2.0/token"
```

The required OAuth2 configuration is up to the user's configured OAuth2
provider. Some providers may require a scope or additional parameters like
an audience, while others may not.

### InfluxDB v3 Cloud Dedicated Configuration

The following is an example Cloud Dedicated configuration:

```toml
[[profile]]
    name = "default"
    product = "dedicated"
    account_id = "dff3ee52-b494-47c1-9e2c-ab59d90d94eb"
    cluster_id = "5827cdeb-b868-4446-b40e-e08de116fddf"
```

#### Migrating from v1 to v2

For users migrating from `influxctl` v1 the differences are:

* Single configuration file called 'config.toml'
* Each profile is specified below a `[[profile]]`
* Specify `product = "dedicated"`

These changes were made to support multiple InfluxDB v3 products with one
configuration file and prevent the need to have multiple files for each
instance.

## CLI

### Debug

To help with debugging or understanding the flow of operations the `--debug`
global flag will print out additional information through the process.

### Additional Profiles

Users can provide additional profiles and reference them with the `--profile`
global option. The name is set in the configuration file via the `name`
parameter. Names must be unique.

### Override Account and Cluster ID

Users of Cloud Dedicated can provide the global `--account-id <ID>` and/or the
`--cluster-id <ID>` to override the value provided in a configuration value.
Values passed via the CLI take precedence over values set in a configuration
file.
