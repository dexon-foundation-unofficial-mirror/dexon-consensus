FROM golang:alpine
MAINTAINER Wei-Ning Huang <w@dexon.org>

RUN apk add --no-cache libstdc++

# Cobinhood vendor base directory.
RUN mkdir -p /opt/dexon/

# Copy data.
COPY build/dexcon-simulation /opt/dexon
COPY build/dexcon-simulation-peer-server /opt/dexon
COPY build/dexcon-simulation-with-scheduler /opt/dexon
COPY entrypoint.sh /opt/dexon
COPY config.toml /opt/dexon

WORKDIR /opt/dexon

ENTRYPOINT ["./entrypoint.sh"]
