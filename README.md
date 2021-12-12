# Linker: HTTP URL Shortener

Linker is a Golang based URL shortening service. This self hosted binary provides fast and efficient HTTP redirection.

Linker is backed by a MySQL database to store name and URL mappings.

## Config

Linker is configured using the following file in "/etc/linker.conf". This file path can be changed using the "-c" flag or by setting the "LINKER_CONFIG" environment variable.

Default Config

```[json]
{
    "key": "",
    "cert": "",
    "listen": "0.0.0.0:80",
    "timeout": 5,
    "default": "https://duckduckgo.com",
    "db": {
        "name": "linker",
        "server": "tcp(localhost:3306)",
        "username": "linker_user",
        "password": "password"
    }
}
```

## Command Line Options

```[text]
Linker - HTTP Web URL Shortener v3
iDigitalFlame & PurpleSec 2020 - 2022 (idigitalflame.com)

Usage:
  -h              Print this help menu.
  -l              List the URL mapping and exit.
  -d              Dump the default configuration and exit.
  -a <name> <URL> Add the specified <name> to <URL> mapping.
  -r <name>       Delete the specified <name> to URL mapping.
  -c <file>       Configuration file path. The environment
                  variable "LINKER_CONFIG" can be used to
                  specify the file path instead.
```
