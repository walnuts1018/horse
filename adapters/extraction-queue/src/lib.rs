use std::{env, error::Error, fmt, time::Duration};

use futures_util::StreamExt;
use lapin::{
    BasicProperties, Channel, Connection, ConnectionProperties, Consumer,
    options::{
        BasicAckOptions, BasicConsumeOptions, BasicNackOptions, BasicPublishOptions,
        BasicQosOptions, QueueDeclareOptions,
    },
    types::{AMQPValue, FieldTable, LongString, ShortString},
};
use tokio::time::timeout;
use uuid::Uuid;

pub const EXTRACTION_QUEUE_NAME: &str = "horse.extraction";
const CONNECTION_TIMEOUT: Duration = Duration::from_secs(10);

#[derive(Debug)]
pub struct QueueError(String);

impl fmt::Display for QueueError {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        formatter.write_str(&self.0)
    }
}

impl Error for QueueError {}

pub struct ExtractionQueue {
    connection: Connection,
    channel: Channel,
}

impl ExtractionQueue {
    pub async fn from_env() -> Result<Option<Self>, QueueError> {
        let Ok(url) = env::var("RABBITMQ_URL") else {
            return Ok(None);
        };
        if url.trim().is_empty() {
            return Ok(None);
        }
        Self::connect(&url).await.map(Some)
    }

    pub async fn connect(url: &str) -> Result<Self, QueueError> {
        if url.trim().is_empty() {
            return Err(QueueError("RabbitMQ URL is empty".to_owned()));
        }
        let separator = if url.contains('?') { '&' } else { '?' };
        let url = format!("{url}{separator}heartbeat=10");
        let connection = timeout(
            CONNECTION_TIMEOUT,
            Connection::connect(&url, ConnectionProperties::default()),
        )
        .await
        .map_err(|_| QueueError("RabbitMQ connection timed out".to_owned()))?
        .map_err(|_| QueueError("RabbitMQ connection failed".to_owned()))?;
        let channel = connection
            .create_channel()
            .await
            .map_err(|_| QueueError("RabbitMQ channel creation failed".to_owned()))?;
        let mut arguments = FieldTable::default();
        arguments.insert(
            ShortString::from("x-queue-type"),
            AMQPValue::LongString(LongString::from("quorum")),
        );
        channel
            .queue_declare(
                EXTRACTION_QUEUE_NAME.into(),
                QueueDeclareOptions {
                    durable: true,
                    ..QueueDeclareOptions::default()
                },
                arguments,
            )
            .await
            .map_err(|_| QueueError("RabbitMQ queue declaration failed".to_owned()))?;
        Ok(Self {
            connection,
            channel,
        })
    }

    pub async fn publish(&self, job_id: Uuid) -> Result<(), QueueError> {
        let body = job_id.to_string();
        self.channel
            .basic_publish(
                "".into(),
                EXTRACTION_QUEUE_NAME.into(),
                BasicPublishOptions::default(),
                body.as_bytes(),
                BasicProperties::default()
                    .with_content_type("text/plain".into())
                    .with_delivery_mode(2),
            )
            .await
            .map_err(|_| QueueError("RabbitMQ job publish failed".to_owned()))?
            .await
            .map_err(|_| QueueError("RabbitMQ job publish was not confirmed".to_owned()))?;
        Ok(())
    }

    pub async fn consume(&self) -> Result<Consumer, QueueError> {
        self.channel
            .basic_qos(1, BasicQosOptions::default())
            .await
            .map_err(|_| QueueError("RabbitMQ consumer qos setup failed".to_owned()))?;
        self.channel
            .basic_consume(
                EXTRACTION_QUEUE_NAME.into(),
                "horse-worker".into(),
                BasicConsumeOptions::default(),
                FieldTable::default(),
            )
            .await
            .map_err(|_| QueueError("RabbitMQ consumer registration failed".to_owned()))
    }

    pub async fn ack(&self, delivery: &lapin::message::Delivery) -> Result<(), QueueError> {
        delivery
            .ack(BasicAckOptions::default())
            .await
            .map(|_| ())
            .map_err(|_| QueueError("RabbitMQ delivery acknowledgement failed".to_owned()))
    }

    pub async fn nack_requeue(
        &self,
        delivery: &lapin::message::Delivery,
    ) -> Result<(), QueueError> {
        delivery
            .nack(BasicNackOptions {
                requeue: true,
                ..BasicNackOptions::default()
            })
            .await
            .map(|_| ())
            .map_err(|_| QueueError("RabbitMQ delivery rejection failed".to_owned()))
    }

    pub async fn close(&self) {
        let _ = self.channel.close(200, "shutdown".into()).await;
        let _ = self.connection.close(200, "shutdown".into()).await;
    }
}

pub async fn next_delivery(
    consumer: &mut Consumer,
) -> Option<Result<lapin::message::Delivery, QueueError>> {
    consumer.next().await.map(|delivery| {
        delivery.map_err(|_| QueueError("RabbitMQ delivery stream failed".to_owned()))
    })
}
