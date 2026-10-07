#!/bin/sh
# Renders the routing tree with only the integrations whose secret files exist.
# Missing Telegram or heartbeat secrets drop that integration; incident ingestion
# always remains. No credential value is written into the rendered file.
set -eu
source=/etc/alertmanager/alertmanager.yml
rendered=/tmp/alertmanager.yml
cp "$source" "$rendered"
if [ -s /run/secrets/telegram_bot_token ] && [ -s /run/secrets/telegram_chat_id ]; then
  api_url="${TELEGRAM_API_URL:-https://api.telegram.org}"
  case "$api_url" in
    https://*|http://telegram-mock:*) ;;
    *) echo "TELEGRAM_API_URL must use https" >&2; exit 1 ;;
  esac
  sed -i "s#__TELEGRAM_API_URL__#$api_url#" "$rendered"
  echo "Telegram notification enabled"
else
  sed -i '/# telegram:begin/,/# telegram:end/d' "$rendered"
  echo "Telegram notification disabled: telegram_bot_token or telegram_chat_id secret missing"
fi
if [ -s /run/secrets/heartbeat_url ]; then
  echo "External heartbeat enabled"
else
  sed -i '/# heartbeat:begin/,/# heartbeat:end/d' "$rendered"
  echo "External heartbeat disabled: heartbeat_url secret missing"
fi
exec /bin/alertmanager --config.file="$rendered" "$@"
