timburr
=======

A MediaWiki event handler for [52Poké Wiki](https://wiki.52poke.com/). It subscribe to specific topics in Kafka, and execute an action in response to each event. Timburr is mainly designed to handle MediaWiki [job queue](https://www.mediawiki.org/wiki/Manual:Job_queue) and page cache purging.

Timburr works in a way similar to Wikimedia [Event Platform](https://wikitech.wikimedia.org/wiki/Event_Platform)'s [change propagation](https://github.com/wikimedia/mediawiki-services-change-propagation), but is more lightweight and appropriate for smaller wikis. It replaces both the [redis job runner](https://github.com/wikimedia/mediawiki-services-jobrunner) we previously used but no longer working after MediaWiki 1.33, and the cache purger [lilycove](https://github.com/mudkipme/lilycove).

## Pre-requisites

A [Kafka](https://kafka.apache.org/) cluster is required. A single-node Kafka cluster running in Docker (KRaft mode, no ZooKeeper) should be good enough for small wikis like 52Poké Wiki.

```bash
docker create --name kafka --net isolated_nw \
  -e KAFKA_CFG_NODE_ID=1 \
  -e KAFKA_CFG_PROCESS_ROLES=broker,controller \
  -e KAFKA_CFG_CONTROLLER_QUORUM_VOTERS=1@kafka:9093 \
  -e KAFKA_CFG_LISTENERS=PLAINTEXT://:9092,CONTROLLER://:9093 \
  -e KAFKA_CFG_ADVERTISED_LISTENERS=PLAINTEXT://kafka:9092 \
  -e KAFKA_CFG_LISTENER_SECURITY_PROTOCOL_MAP=PLAINTEXT:PLAINTEXT,CONTROLLER:PLAINTEXT \
  -e KAFKA_CFG_CONTROLLER_LISTENER_NAMES=CONTROLLER \
  -e KAFKA_CFG_INTER_BROKER_LISTENER_NAME=PLAINTEXT \
  -e KAFKA_CFG_OFFSETS_TOPIC_REPLICATION_FACTOR=1 \
  -v kafka:/bitnami/kafka \
  --restart=always \
  bitnami/kafka:latest
```

## Configuration

### Event Producer

Timburr includes a small HTTP server that accepts events and writes them to Kafka. Point your EventBus endpoint to the Timburr server and it will publish each request body to the topic configured by `options.topicKey` (or `options.defaultTopic` if missing). The request body can be a single JSON object or an array of objects.

Example:

```
POST http://<timburr-host>:<timburr-port>/
Content-Type: application/json

{"meta":{"stream":"mediawiki.job.refreshLinks"}, ...}
```

### MediaWiki

Timburr requires a [modified version of EventBus](https://github.com/mudkipme/mediawiki-extensions-EventBus) extension<sup>[1](#why-eventbus)</sup> and MediaWiki 1.43.

```php
$wgJobRunRate = 0;
$wgJobTypeConf['default'] = [ 'class' => '\\MediaWiki\\Extension\\EventBus\\Adapters\\JobQueue\\JobQueueEventBus', 'readOnlyReason' => false ];

wfLoadExtension( 'EventBus' );
$wgEnableEventBus = 'TYPE_ALL';
$wgEventServices = [
    'eventbus' => [ 'url' => 'http://<timburr-host>:<timburr-port>', 'timeout' => 10 ],
];

// only needed to handle cache purging
$wgEventRelayerConfig = [
    'cdn-url-purges' => [
        'class' => \MediaWiki\Extension\EventBus\Adapters\EventRelayer\CdnPurgeEventRelayer::class,
        'stream' => 'cdn-url-purges'
    ],
];

```

### Timburr

Write a `conf/config.yml` file with the following example:

```yaml
kafka:
  brokerList: "<kafka-broker>:9092"

options:
  groupIDPrefix: timburr-
  metadataWatchRefreshInterval: 10000

jobRunner:
  endpoint: http://<mediawiki-host>/rest.php/eventbus/v0/internal/job/execute
  excludeFields: ["host", "headers", "@timestamp", "@version"] # exclude fields added by upstream producer

purge: # only needed to handle cache purging
  expiry: 86400000  # cache expiry time, in milliseconds
  cfZoneID: <cloudflare zone id> # only needed if cloudflare CDN is used
  cfToken: <cloudflare token>
  entries:
  - host: <mediawiki-host> # entry for purging page cache
    pathMatch: "^/wiki/" # only handle URLs with paths matching this regex (optional)
    timeout: 2000 # request timeout in milliseconds (optional, defaults to 2000)
    method: PURGE # method to purge cache, see [libnginx-mod-http-cache-purge](https://packages.debian.org/buster/libnginx-mod-http-cache-purge) or [ngx_cache_purge](https://github.com/FRiCKLE/ngx_cache_purge) if nginx is used
    variants:
    - zh # only needed if cache keys contain language variants
    - zh-hans
    - zh-hant
    uris:
    - "http://<frontend-server>#url##variants#"
    - "http://<frontend-server>#url##variants#mobile" # only needed if cache keys differ between desktop and mobile
    headers:
      host: <mediawiki-host>
      x-purge-date: "#date#" # optional: RFC3339 timestamp from meta.dt
  - host: <image-host> # entries for purging image cache
    method: PURGE
    uris:
    - "http://<frontend-server>#url#"
    - "http://<frontend-server>/webp-cache#url#" # see [malasada](https://github.com/mudkipme/malasada)
    headers:
      host: <image-host>
  - host: <image-host> # entry for [malasada](https://github.com/mudkipme/malasada)
    method: DELETE
    uris:
    - "https://<api-gateway-endpoint>/<api-gateway-stage>/webp#url#"
  - host: <image-host> # only needed if cloudflare CDN is used
    method: Cloudflare
    uris:
    - "https://<cloudflare-host>#url#"

rules:
- name: basic
  topic: /^mediawiki\.job\./
  excludeTopics:
  - mediawiki.job.AssembleUploadChunks
  - mediawiki.job.PublishStashedFile
  - mediawiki.job.uploadFromUrl
  - mediawiki.job.cirrusSearchLinksUpdate
  - mediawiki.job.htmlCacheUpdate
  - mediawiki.job.refreshLinks

- name: low-priority
  topics:
  - mediawiki.job.cirrusSearchLinksUpdate
  - mediawiki.job.htmlCacheUpdate
  - mediawiki.job.refreshLinks

- name: upload
  topics:
  - mediawiki.job.AssembleUploadChunks
  - mediawiki.job.PublishStashedFile
  - mediawiki.job.uploadFromUrl

- name: purge # only needed to handle cache purging
  topic: cdn-url-purges
  taskType: purge
  rateLimit: 5 # only handle 5 events in 10000 milliseconds in this rule group
  rateInterval: 10000
```

## Installation

Golang is required to compile timburr. It is recommended to run timburr via a [Docker image](https://github.com/52poke/timburr/pkgs/container/timburr).

```bash
docker create --name timburr --net isolated_nw \
  -v <path-to-config>/timburr.yml:/app/conf/config.yml \
  --restart=always \
  ghcr.io/52poke/timburr
```

## LICENSE

This project is under [BSD-3-Clause](LICENSE).

52Poké (神奇宝贝部落格/神奇寶貝部落格, 神奇宝贝百科/神奇寶貝百科) is a Chinese-language Pokémon fan site. Neither the name of 52Poké nor the names of the contributors may be used to endorse any usage of codes under this project.

Pokémon ©2020 Pokémon. ©1995-2020 Nintendo/Creatures Inc./GAME FREAK inc. 52Poké and this project is not affiliated with any Pokémon-related companies.

---

<a name="why-eventbus">1</a>: `Special:RunSingleJob` verifies `mediawiki_signature` in event data. However the order of keys in event structure may not be preserved, thus results in invalid signature. This patch sorts keys before generating and verifying job signature.
