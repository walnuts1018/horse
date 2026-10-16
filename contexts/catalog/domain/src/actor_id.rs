use uuid::Uuid;

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct ActorId(String);

impl ActorId {
    pub fn as_str(&self) -> &str {
        &self.0
    }
}

impl From<Uuid> for ActorId {
    fn from(value: Uuid) -> Self {
        Self(value.to_string())
    }
}
