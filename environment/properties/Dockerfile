FROM benchflow/base-images:dev

MAINTAINER Vincenzo FERME <info@vincenzoferme.it>

ENV COLLECTOR_NAME properties
ENV COLLECTOR_VERSION v-dev

RUN apk --update add wget gzip && \
    wget -q --no-check-certificate -O /app/$COLLECTOR_NAME https://github.com/benchflow/collectors/releases/download/$COLLECTOR_VERSION/$COLLECTOR_NAME && \
    chmod +x /app/$COLLECTOR_NAME && \
    apk del --purge wget && \
    rm -rf /var/cache/apk/*

COPY ./services/300-environment-properties-collector.conf /apps/chaperone.d/300-environment-properties-collector.conf

EXPOSE 8080