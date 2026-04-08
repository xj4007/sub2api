FROM postgres:18-alpine

RUN apk add --no-cache bash python3 py3-pip curl su-exec && \
    pip3 install --break-system-packages --no-cache-dir "patroni[etcd3]" "psycopg[binary]"

COPY entrypoint.sh /entrypoint.sh
RUN chmod +x /entrypoint.sh

ENTRYPOINT ["/entrypoint.sh"]
