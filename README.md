# iot-reader

A client for [sula7/iot-server](https://github.com/sula7/iot-server). Dials host:port server, transfers data (payload)
and pings in the background.

### Env vars

````shell
DIAL_ADDRESS=host:port # address dial to
LOG_LEVEL=debug # default is info
````

### Packet structure

#### Header

| Protocol version | Packet type | Reserved | Reserved |
|:----------------:|-------------|----------|----------|
|        1         | 10/11/20    | 0        | 0        |

Protocol versions:

* 1 - current version

In case of receiving unknown protocol version from server **TBD** (currently accepts any)

Packet types:

* 10 - ping (incoming) every 5 seconds
* 11 - pong (response) expects from server after ping
* 20 - data transfer (bi-directional)

In case of receiving unknown packet type from server will log an error.
If no pong packets received after 10 pings (**TBD** sequentially) the client will stop.

#### Body

Consists of 10 bytes. Structure **TBD** (currently not implemented)
