ARG PHX_NEW_VERSION=1.8.9
ARG ELIXIR_IMAGE=hexpm/elixir:1.20.2-erlang-28.5.0.3-alpine-3.23.5@sha256:6f03034e254126f063959873d8d3b811ee92abaabab27b62c53982c4a1034e39
ARG ALPINE_IMAGE=alpine:3.23.5@sha256:fd791d74b68913cbb027c6546007b3f0d3bc45125f797758156952bc2d6daf40

FROM ${ELIXIR_IMAGE} AS source
ARG PHX_NEW_VERSION
RUN apk add --no-cache build-base git
RUN mix local.hex --force \
    && mix local.rebar --force \
    && mix archive.install hex phx_new ${PHX_NEW_VERSION} --force
WORKDIR /workspace
RUN mix phx.new app --database sqlite3 --no-mailer --no-dashboard --no-install --no-agents-md --no-version-check
WORKDIR /workspace/app
RUN mix deps.get \
    && mix phx.gen.live Catalog Item items name:string --no-scope
RUN sed -i '/get "\\/", PageController, :home/a\    live "/items/:id/edit", ItemLive.Form, :edit' lib/app_web/router.ex \
    && sed -i '/get "\\/", PageController, :home/a\    live "/items/:id", ItemLive.Show, :show' lib/app_web/router.ex \
    && sed -i '/get "\\/", PageController, :home/a\    live "/items/new", ItemLive.Form, :new' lib/app_web/router.ex \
    && sed -i '/get "\\/", PageController, :home/a\    live "/items", ItemLive.Index, :index' lib/app_web/router.ex \
    && mix phx.gen.release

FROM source AS build
RUN MIX_ENV=prod mix deps.get --only prod
RUN MIX_ENV=prod mix compile
RUN MIX_ENV=prod mix assets.deploy
RUN MIX_ENV=prod mix release

FROM ${ALPINE_IMAGE} AS runtime
RUN apk add --no-cache ca-certificates libgcc libstdc++ ncurses-libs openssl
ENV PHX_SERVER=true \
    PORT=4000 \
    DATABASE_PATH=/app/data/phoenix_liveview.sqlite3
WORKDIR /app
RUN mkdir -p /app/data \
    && chown -R 65534:65534 /app
COPY --from=build --chown=65534:65534 /workspace/app/_build/prod/rel/app ./
USER 65534:65534
EXPOSE 4000
CMD ["sh", "-c", "/app/bin/migrate && exec /app/bin/server"]
