use async_graphql::{InputValueError, InputValueResult, Scalar, ScalarType, Value};
use chrono::{DateTime as ChronoDateTime, SecondsFormat, Utc};

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct Long(pub i64);

#[Scalar(name = "Long")]
impl ScalarType for Long {
    fn parse(value: Value) -> InputValueResult<Self> {
        match value {
            Value::Number(value) => value
                .as_i64()
                .map(Self)
                .ok_or_else(|| InputValueError::expected_type(Value::Number(value))),
            value => Err(InputValueError::expected_type(value)),
        }
    }

    fn to_value(&self) -> Value {
        Value::Number(self.0.into())
    }
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct DateTime(String);

impl DateTime {
    pub fn parse_utc(&self) -> Result<ChronoDateTime<Utc>, chrono::ParseError> {
        ChronoDateTime::parse_from_rfc3339(&self.0).map(|value| value.with_timezone(&Utc))
    }

    pub fn as_str(&self) -> &str {
        &self.0
    }
}

impl From<ChronoDateTime<Utc>> for DateTime {
    fn from(value: ChronoDateTime<Utc>) -> Self {
        Self(value.to_rfc3339_opts(SecondsFormat::AutoSi, true))
    }
}

#[Scalar(name = "DateTime")]
impl ScalarType for DateTime {
    fn parse(value: Value) -> InputValueResult<Self> {
        match value {
            Value::String(value) if ChronoDateTime::parse_from_rfc3339(&value).is_ok() => {
                Ok(Self(value))
            }
            value => Err(InputValueError::expected_type(value)),
        }
    }

    fn to_value(&self) -> Value {
        Value::String(self.0.clone())
    }
}
