# Hoosat (HTN) Stratum Solo Mining Bridge in a Box

This is a [forked](https://github.com/HoosatNetwork/htn-stratum-bridge) lightweight daemon that allows mining to a local (or remote) hoosat node using stratum-base miners.

This daemon is confirmed working with the miners below 
* [hoo_cpu](https://htn.foztor.net/)
* [hoo_gpu](https://htn.foztor.net/)
* [hoo_gpu_amd](https://htn.foztor.net/)
* [hoo_cpu_arm](https://htn.foztor.net/)
* [hoodroid](https://htn.foztor.net/)
* [Hoominer](https://github.com/HoosatNetwork/hoominer/releases)

Huge shoutout to https://github.com/HoosatNetwork/htn-stratum-bridge and
https://github.com/onemorebsmith/kaspa-stratum-bridgev and https://github.com/KaffinPX/KStratum for the work


# Features:

## Bridge Fee (Optional)

The bridge supports an optional **public bridge fee** feature that allows operators to divert a small percentage of block templates to a configured bridge address. This feature is **disabled by default**.

### How it works:
- When enabled, a configurable percentage (parts per 10,000) of `getblocktemplate` requests to the daemon use the bridge payout address instead of the miner's address
- Selection is deterministic and server-side using HMAC-SHA256 with a secret salt to prevent miner manipulation
- Only the payout address in the GBT request is modified; miners receive work normally and submit shares as usual
- The feature includes logging and Prometheus metrics (`htn_diverted_gbt_total`) for transparency

### Configuration:

Add to your `config.yaml`:

```yaml
bridge_fee:
  enabled: false              # Set to true to enable (default: false)
  rate_ppm: 50               # Parts per 10000 (50 = 0.5%, 100 = 1%, 500 = 5%)
  address: "hoosat:qq2g85qrj2k4xs80y32v69kjn7nr49khyrack9mpd3gy54vfp8ja53ws4yezz" # Replace with your own fee wallet.  Obviously....
  server_salt: ""            # REQUIRED: Set a random secret for production
```

**Important:** 
- The `server_salt` must be configured for the feature to activate. Generate one with: `openssl rand -hex 32`
- Without a server salt, the feature remains disabled even if `enabled: true`
- Default rate is 0.5% (50 ppm) when enabled

## WebUI (Optional)

In order to enable the **WebUI** you must specify the following in your `config.yaml`
```yaml
web_port: :8888  # Port to bind the WebUI to - or could be 127.0.0.1:8888 or 0.0.0.0:8888 or any other valid IP binding
stratum_addr: "stratum+tcp://htn.foztor.net:5555  # This is the stratum address the bridge is listening on - it is displayed on the welcome page as an instruction to the miners
```

### Reverse Proxy (Optional)

You probably want to front the WebUI with a reverse proxy if only to provide TLS.
For instance with NGINX (and a friendly reloading page for when the pool is down, provide appropriate content in /var/www/html/refresh.html (or whatever)
Adjust IP adddress and ports to match config.yaml and reflect your setup.

```nginx
server {
    server_name uk-pool.htn.foztor.net;

    # API must never return the HTML refresh page
    location ^~ /api/ {
        proxy_pass http://192.168.1.10:8888;
        proxy_intercept_errors off;

        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;

        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection "upgrade";
    }

    location / {
        proxy_pass http://192.168.1.10:8888;

        proxy_intercept_errors on;
        error_page 502 504 =200 /refresh.html;

        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;

        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection "upgrade";
    }

    location = /refresh.html {
        root /var/www/html;
        internal;
    }

    listen 443 ssl;
    ssl_certificate /etc/letsencrypt/live/uk-pool.htn.foztor.net/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/uk-pool.htn.foztor.net/privkey.pem;
    include /etc/letsencrypt/options-ssl-nginx.conf;
    ssl_dhparam /etc/letsencrypt/ssl-dhparams.pem;
}
```

Example refresh.html referenced above:
```
<!DOCTYPE html>
<html>
  <head>
    <meta http-equiv="refresh" content="2">
    <title>Service Starting...</title>
  </head>
  <body>
    <p>Service is currently unavailable. Retrying in 2 seconds...</p>
  </body>
</html>

```


## Manual build

Install go 1.25 using whatever package manager is approprate for your system

  

run `cd cmd/htnbridge;go build .`

  

Modify the config file in ./cmd/bridge/config.yaml with your setup, the file comments explain the various flags

  

run `./htnbridge --solo` in the `cmd/hoosatbridge` directory

  

all-in-one (build + run) `cd cmd/htnbridge/;go build .;./htnbridge`
