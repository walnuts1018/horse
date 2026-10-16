use serde_json::Value;

#[derive(Debug, Clone, PartialEq)]
pub struct UiChangeDescriptor {
    pub event_type: &'static str,
    pub entity_kind: &'static str,
    pub changed_fields: Vec<String>,
    pub payload: Value,
}

impl UiChangeDescriptor {
    pub fn new(
        event_type: &'static str,
        entity_kind: &'static str,
        changed_fields: impl IntoIterator<Item = impl Into<String>>,
    ) -> Self {
        Self {
            event_type,
            entity_kind,
            changed_fields: changed_fields.into_iter().map(Into::into).collect(),
            payload: Value::Object(Default::default()),
        }
    }

    pub fn with_payload(mut self, payload: Value) -> Self {
        self.payload = payload;
        self
    }
}
