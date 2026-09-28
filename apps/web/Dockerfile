FROM node:22-alpine AS build
WORKDIR /app
COPY package.json package-lock.json ./
COPY apps/web/package.json apps/web/package.json
COPY apps/web apps/web
ARG API_URL=http://api-gateway:8080
ENV API_URL=$API_URL
ENV NEXT_TELEMETRY_DISABLED=1
# Keep dependencies, compiler cache and intermediate Next output in memory.
# Persist only the files needed by the standalone runtime.
RUN --mount=type=tmpfs,target=/app/node_modules \
    --mount=type=tmpfs,target=/root/.npm \
    --mount=type=tmpfs,target=/app/apps/web/.next \
    npm ci && npm run build && mkdir -p /output && \
    cp -a apps/web/.next/standalone /output/standalone && \
    cp -a apps/web/.next/static /output/static
FROM node:22-alpine
WORKDIR /app
ENV NODE_ENV=production
ENV NEXT_TELEMETRY_DISABLED=1
ENV HOSTNAME=0.0.0.0
ENV PORT=3000
ENV HOME=/tmp
COPY --from=build --chown=node:node /output/standalone ./
COPY --from=build --chown=node:node /output/static ./apps/web/.next/static
# OpenShift may assign an arbitrary UID in group 0; Next's runtime cache must
# remain writable without depending on the image's named node user.
RUN mkdir -p /app/apps/web/.next/cache && chgrp -R 0 /app/apps/web/.next/cache && chmod -R g=u /app/apps/web/.next/cache
USER node
EXPOSE 3000
CMD ["node", "apps/web/server.js"]
