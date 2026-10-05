# Uptime check do /healthz pelo domínio público, de três regiões, e alerta por
# e-mail quando ele falha. Os logs (JSON do slog) e as métricas dos pods já vão
# para o Cloud Logging e o Cloud Monitoring pelo GKE, sem agente extra.

resource "google_monitoring_uptime_check_config" "api" {
  display_name = "parceiros-${var.ambiente} /healthz"
  timeout      = "10s"
  period       = "60s"

  http_check {
    path         = "/healthz"
    port         = 443
    use_ssl      = true
    validate_ssl = true
  }

  monitored_resource {
    type = "uptime_url"
    labels = {
      project_id = var.projeto
      host       = var.dominio
    }
  }

  selected_regions = ["SOUTH_AMERICA", "USA_VIRGINIA", "USA_OREGON"]
  depends_on       = [google_project_service.apis]
}

resource "google_monitoring_notification_channel" "email" {
  count        = var.email_alertas == "" ? 0 : 1
  display_name = "Alertas parceiros-${var.ambiente}"
  type         = "email"
  labels = {
    email_address = var.email_alertas
  }
}

resource "google_monitoring_alert_policy" "fora_do_ar" {
  count        = var.email_alertas == "" ? 0 : 1
  display_name = "parceiros-${var.ambiente} fora do ar"
  combiner     = "OR"

  conditions {
    display_name = "uptime check falhando"
    condition_threshold {
      filter          = "metric.type=\"monitoring.googleapis.com/uptime_check/check_passed\" AND resource.type=\"uptime_url\" AND metric.label.check_id=\"${google_monitoring_uptime_check_config.api.uptime_check_id}\""
      comparison      = "COMPARISON_GT"
      threshold_value = 1
      duration        = "120s"
      aggregations {
        alignment_period     = "60s"
        per_series_aligner   = "ALIGN_NEXT_OLDER"
        cross_series_reducer = "REDUCE_COUNT_FALSE"
        group_by_fields      = ["resource.label.host"]
      }
      trigger {
        count = 1
      }
    }
  }

  notification_channels = [google_monitoring_notification_channel.email[0].id]
}
