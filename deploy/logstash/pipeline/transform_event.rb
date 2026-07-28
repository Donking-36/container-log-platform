# 将 Filebeat 事件转换成 Gin 内部接收接口要求的字段结构。
def filter(event)
  raw_event = event.to_hash

  source_event_id = event.get("[producer][source_event_id]")
  agent_id = event.get("[agent][id]")
  container_name = event.get("[container][name]")
  container_id = event.get("[container][id]")
  service = event.get("[service][name]")
  level = event.get("[producer][level]")

  message = event.get("[producer][message]")
  message = event.get("message") if message.nil?

  source =
    if event.get("[platform][input_kind]") == "file"
      "file"
    else
      event.get("stream")
    end

  log_path = event.get("[log][file][path]")
  log_offset = event.get("[log][offset]")
  logged_at = event.get("[producer][logged_at]")

  string_values = [
    source_event_id,
    agent_id,
    container_name,
    container_id,
    service,
    level,
    message,
    source,
    log_path,
    logged_at
  ]

  invalid_string = string_values.any? do |value|
    !value.nil? && !value.is_a?(String)
  end

  invalid_log_offset =
    !log_offset.nil? && !log_offset.is_a?(Integer)

  # Gin会一次解码整个JSON数组。先隔离类型错误事件，
  # 避免一条坏事件使同批合法事件一起收到HTTP 400。
  if invalid_string || invalid_log_offset
    event.tag("_ingestion_transform_error")
    return [event]
  end

  payload = {
    "source_event_id" => source_event_id,
    "agent_id" => agent_id,
    "container_name" => container_name,
    "container_id" => container_id,
    "service" => service,
    "level" => level,
    "message" => message,
    "source" => source,
    "log_path" => log_path,
    "log_offset" => log_offset,
    "logged_at" => logged_at,
    "raw_event" => raw_event
  }

  # 删除原来的 Filebeat 根字段，例如 @timestamp、producer、agent。
  event.to_hash.keys.each do |field|
    event.remove(field)
  end

  # nil 表示来源没有提供该可选字段，不需要发送给 API。
  payload.each do |field, value|
    event.set(field, value) unless value.nil?
  end

  [event]
end

test "maps container event to ingestion payload" do
  in_event do
    {
      "producer" => {
        "source_event_id" => "container-event-000001",
        "level" => "INFO",
        "message" => "container event",
        "logged_at" => "2026-07-27T10:00:00Z"
      },
      "agent" => {
        "id" => "filebeat-agent-001"
      },
      "container" => {
        "id" => "container-id-001",
        "name" => "log-producer"
      },
      "service" => {
        "name" => "log-producer"
      },
      "platform" => {
        "input_kind" => "container"
      },
      "stream" => "stdout",
      "log" => {
        "file" => {
          "path" => "/var/lib/docker/containers/example/example-json.log"
        },
        "offset" => 42
      }
    }
  end

  expect("keeps only fields accepted by the API") do |events|
    payload = events.first.to_hash

    expected_fields = %w[
      source_event_id
      agent_id
      container_name
      container_id
      service
      level
      message
      source
      log_path
      log_offset
      logged_at
      raw_event
    ]

    events.length == 1 &&
      payload.keys.sort == expected_fields.sort &&
      payload["source"] == "stdout" &&
      payload["message"] == "container event"
  end

  expect("preserves the original event") do |events|
    raw_event = events.first.get("raw_event")

    raw_event["producer"]["source_event_id"] ==
      "container-event-000001" &&
      raw_event["container"]["id"] == "container-id-001"
  end
end

test "maps file input source" do
  in_event do
    {
      "producer" => {
        "source_event_id" => "file-event-000001",
        "level" => "WARN",
        "message" => "file event",
        "logged_at" => "2026-07-27T10:01:00Z"
      },
      "agent" => {
        "id" => "filebeat-agent-001"
      },
      "container" => {
        "name" => "log-producer"
      },
      "service" => {
        "name" => "log-producer"
      },
      "platform" => {
        "input_kind" => "file"
      },
      "log" => {
        "file" => {
          "path" => "/logs/producer.ndjson"
        },
        "offset" => 128
      }
    }
  end

  expect("uses file as the normalized source") do |events|
    payload = events.first.to_hash

    events.length == 1 &&
      payload["source"] == "file" &&
      payload["log_path"] == "/logs/producer.ndjson" &&
      payload["log_offset"] == 128
  end
end

test "isolates incompatible field types" do
  in_event do
    {
      "producer" => {
        "source_event_id" => 123,
        "level" => "INFO",
        "message" => "invalid source event ID type",
        "logged_at" => "2026-07-27T10:02:00Z"
      },
      "agent" => {
        "id" => "filebeat-agent-001"
      },
      "container" => {
        "name" => "log-producer"
      },
      "service" => {
        "name" => "log-producer"
      },
      "platform" => {
        "input_kind" => "file"
      },
      "log" => {
        "file" => {
          "path" => "/logs/producer.ndjson"
        },
        "offset" => 256
      }
    }
  end

  expect("tags the event without rebuilding its fields") do |events|
    event = events.first
    tags = event.get("tags")

    events.length == 1 &&
      !tags.nil? &&
      tags.include?("_ingestion_transform_error") &&
      event.get("[producer][source_event_id]") == 123 &&
      event.get("raw_event").nil?
  end
end
