FROM golang:1.22.1 as builder

WORKDIR /usr/src/app
COPY . .
RUN go build

FROM debian:bookworm-slim
COPY --from=builder /usr/src/app/matrix-synapse-diskspace-janitor /usr/local/bin/matrix-synapse-diskspace-janitor
CMD [ "matrix-synapse-diskspace-janitor" ]