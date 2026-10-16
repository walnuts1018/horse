use std::{env, error::Error, io};

use http::HeaderMap;
use opentelemetry::{
    global,
    propagation::{TextMapCompositePropagator, TextMapPropagator},
    trace::TracerProvider as _,
};
use opentelemetry_http::HeaderExtractor;
use opentelemetry_propagator_b3::Propagator as B3Propagator;
use opentelemetry_sdk::{
    Resource,
    metrics::SdkMeterProvider,
    propagation::{BaggagePropagator, TraceContextPropagator},
    trace::SdkTracerProvider,
};
use tracing_opentelemetry::OpenTelemetrySpanExt;
use tracing_subscriber::{EnvFilter, Layer, layer::SubscriberExt, util::SubscriberInitExt};

pub struct Telemetry {
    tracer_provider: SdkTracerProvider,
    meter_provider: SdkMeterProvider,
}

impl Telemetry {
    pub fn init(
        service_name: &'static str,
        filter: EnvFilter,
    ) -> Result<Self, Box<dyn Error + Send + Sync>> {
        let resource = Resource::builder().with_service_name(service_name).build();
        let mut tracer_builder = SdkTracerProvider::builder().with_resource(resource.clone());
        match signal_exporter("TRACES")? {
            Exporter::Otlp => {
                let exporter = opentelemetry_otlp::SpanExporter::builder().build()?;
                let processor = opentelemetry_sdk::trace::span_processor_with_async_runtime::BatchSpanProcessor::builder(
                    exporter,
                    opentelemetry_sdk::runtime::Tokio,
                )
                .build();
                tracer_builder = tracer_builder.with_span_processor(processor);
            }
            Exporter::Console => {
                tracer_builder = tracer_builder
                    .with_simple_exporter(opentelemetry_stdout::SpanExporter::default());
            }
            Exporter::None => {}
        }
        let tracer_provider = tracer_builder.build();
        let tracer = tracer_provider.tracer(service_name);
        let mut meter_builder = SdkMeterProvider::builder().with_resource(resource);
        match signal_exporter("METRICS")? {
            Exporter::Otlp => {
                meter_builder = meter_builder
                    .with_periodic_exporter(opentelemetry_otlp::MetricExporter::builder().build()?);
            }
            Exporter::Console => {
                meter_builder = meter_builder
                    .with_periodic_exporter(opentelemetry_stdout::MetricExporter::default());
            }
            Exporter::None => {}
        }
        let meter_provider = meter_builder.build();

        global::set_tracer_provider(tracer_provider.clone());
        global::set_meter_provider(meter_provider.clone());
        global::set_text_map_propagator(configure_propagator()?);

        let otel_layer = tracing_opentelemetry::layer().with_tracer(tracer);
        let fmt_layer = tracing_subscriber::fmt::layer().json().with_filter(filter);
        tracing_subscriber::registry()
            .with(otel_layer)
            .with(fmt_layer)
            .try_init()?;

        Ok(Self {
            tracer_provider,
            meter_provider,
        })
    }

    pub fn shutdown(self) -> Result<(), Box<dyn Error + Send + Sync>> {
        self.meter_provider.shutdown().map_err(|error| {
            io::Error::other(format!("shut down OpenTelemetry meter provider: {error}"))
        })?;
        self.tracer_provider.shutdown().map_err(|error| {
            io::Error::other(format!("shut down OpenTelemetry tracer provider: {error}"))
        })?;
        Ok(())
    }
}

pub fn set_parent_from_headers(span: &tracing::Span, headers: &HeaderMap) {
    let parent =
        global::get_text_map_propagator(|propagator| propagator.extract(&HeaderExtractor(headers)));
    let _ = span.set_parent(parent);
}

enum Exporter {
    None,
    Otlp,
    Console,
}

fn signal_exporter(signal: &str) -> Result<Exporter, Box<dyn Error + Send + Sync>> {
    let key = format!("OTEL_{signal}_EXPORTER");
    if let Ok(value) = env::var(&key)
        && !value.is_empty()
    {
        return match value.as_str() {
            "none" => Ok(Exporter::None),
            "otlp" => Ok(Exporter::Otlp),
            "console" => Ok(Exporter::Console),
            _ => Err(io::Error::new(
                io::ErrorKind::InvalidInput,
                format!("unsupported {key} value: {value}"),
            )
            .into()),
        };
    }
    if env::var(format!("OTEL_EXPORTER_OTLP_{signal}_ENDPOINT"))
        .is_ok_and(|value| !value.is_empty())
        || env::var("OTEL_EXPORTER_OTLP_ENDPOINT").is_ok_and(|value| !value.is_empty())
    {
        Ok(Exporter::Otlp)
    } else {
        Ok(Exporter::None)
    }
}

fn configure_propagator() -> Result<TextMapCompositePropagator, Box<dyn Error + Send + Sync>> {
    let configured = env::var("OTEL_PROPAGATORS")
        .ok()
        .filter(|value| !value.trim().is_empty())
        .unwrap_or_else(|| "tracecontext,baggage".to_owned());
    if configured.trim() == "none" {
        return Ok(TextMapCompositePropagator::new(Vec::new()));
    }

    let mut propagators: Vec<Box<dyn TextMapPropagator + Send + Sync>> = Vec::new();
    for name in configured.split(',').map(str::trim) {
        match name {
            "tracecontext" => propagators.push(Box::new(TraceContextPropagator::new())),
            "baggage" => propagators.push(Box::new(BaggagePropagator::new())),
            "b3" | "b3multi" => propagators.push(Box::new(B3Propagator::new())),
            _ => {
                return Err(io::Error::new(
                    io::ErrorKind::InvalidInput,
                    format!("unsupported OTEL_PROPAGATORS value: {name}"),
                )
                .into());
            }
        }
    }
    Ok(TextMapCompositePropagator::new(propagators))
}
