# NATS with JetStream on /data, Mikoshi's broker.
# See architecture/standards/go-elemental/principles/release-and-ci.md
FROM nats:2.15.3-alpine

COPY nats-server.conf /etc/nats/nats-server.conf

CMD ["-c", "/etc/nats/nats-server.conf"]

HEALTHCHECK --start-period=30s --start-interval=1s --interval=5s --timeout=3s --retries=5 \
    CMD ["wget", "-q", "-O", "/dev/null", "http://127.0.0.1:8222/healthz?js-enabled-only=true"]
