Watchtower provides an HTTP API mode that enables HTTP endpoints for managing containers and schedules. The current available endpoints are:

### Container Endpoints

-   `GET /api/v1/containers` - List all containers
-   `POST /api/v1/containers/:id/pause` - Pause a running container
-   `POST /api/v1/containers/:id/resume` - Resume a paused container

### Update Endpoints

-   `POST /v1/update` - Triggers an update for all containers monitored by this Watchtower instance

### Schedule Endpoints

-   `GET /api/v1/schedule` - Get the current cron schedule
-   `PUT /api/v1/schedule` - Update the cron schedule at runtime

---

To enable this mode, use the flag `--http-api-update`. For example, in a Docker Compose config file:

```yaml
version: '3'

services:
  app-monitored-by-watchtower:
    image: myapps/monitored-by-watchtower
    labels:
      - "com.centurylinklabs.watchtower.enable=true"

  watchtower:
    image: containrrr/watchtower
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock
    command: --debug --http-api-update
    environment:
      - WATCHTOWER_HTTP_API_TOKEN=mytoken
    labels:
      - "com.centurylinklabs.watchtower.enable=false"
    ports:
      - 8080:8080
```

By default, enabling this mode prevents periodic polls (i.e. what is specified using `--interval` or `--schedule`). To run periodic updates regardless, pass `--http-api-periodic-polls`.

The HTTP API port defaults to `8080` but can be configured via the `--http-api-port` flag or `WATCHTOWER_HTTP_PORT` environment variable.

Notice that there is an environment variable named WATCHTOWER_HTTP_API_TOKEN. To prevent external services from accidentally triggering image updates, all of the requests have to contain a "Token" field, valued as the token defined in WATCHTOWER_HTTP_API_TOKEN, in their headers. In this case, there is a port bind to the host machine, allowing to request localhost:8080 to reach Watchtower. The following `curl` command would trigger an image update:

```bash
curl -H "Authorization: Bearer mytoken" localhost:8080/v1/update
```

---

In order to update only certain images, the image names can be provided as URL query parameters. The following `curl` command would trigger an update for the images `foo/bar` and `foo/baz`:

```bash
curl -H "Authorization: Bearer mytoken" localhost:8080/v1/update?image=foo/bar,foo/baz
```
