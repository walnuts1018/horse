use household_application::{
    mail_forwarding::{Command, ForwardingAddress, IssueCommand, Status},
    mail_forwarding_handler::{MailForwardingHandler, MailForwardingHandlerError},
};

use crate::PgPool;

pub struct PostgresMailForwardingHandler {
    pool: PgPool,
}

impl PostgresMailForwardingHandler {
    pub fn new(pool: PgPool) -> Self {
        Self { pool }
    }
}

impl MailForwardingHandler for PostgresMailForwardingHandler {
    fn active<'a>(
        &'a self,
        command: Command,
    ) -> std::pin::Pin<
        Box<
            dyn std::future::Future<Output = Result<Status, MailForwardingHandlerError>>
                + Send
                + 'a,
        >,
    > {
        Box::pin(async move {
            crate::mail_forwarding::active(&self.pool, command)
                .await
                .map_err(map_error)
        })
    }

    fn issue<'a>(
        &'a self,
        command: IssueCommand,
    ) -> std::pin::Pin<
        Box<
            dyn std::future::Future<Output = Result<ForwardingAddress, MailForwardingHandlerError>>
                + Send
                + 'a,
        >,
    > {
        Box::pin(async move {
            crate::mail_forwarding::issue(&self.pool, command)
                .await
                .map_err(map_error)
        })
    }

    fn rotate<'a>(
        &'a self,
        command: IssueCommand,
    ) -> std::pin::Pin<
        Box<
            dyn std::future::Future<Output = Result<ForwardingAddress, MailForwardingHandlerError>>
                + Send
                + 'a,
        >,
    > {
        Box::pin(async move {
            crate::mail_forwarding::rotate(&self.pool, command)
                .await
                .map_err(map_error)
        })
    }

    fn revoke<'a>(
        &'a self,
        command: Command,
    ) -> std::pin::Pin<
        Box<
            dyn std::future::Future<Output = Result<Status, MailForwardingHandlerError>>
                + Send
                + 'a,
        >,
    > {
        Box::pin(async move {
            crate::mail_forwarding::revoke(&self.pool, command)
                .await
                .map_err(map_error)
        })
    }
}

fn map_error(error: crate::mail_forwarding::MailForwardingError) -> MailForwardingHandlerError {
    use crate::mail_forwarding::MailForwardingError as Error;
    match error {
        Error::InvalidUuid(_) | Error::Workflow(_) => MailForwardingHandlerError::InvalidInput,
        Error::HouseholdNotFound | Error::ForwardingNotFound => {
            MailForwardingHandlerError::NotFound
        }
        Error::Conflict => MailForwardingHandlerError::Conflict,
        Error::Pool(message) => MailForwardingHandlerError::Infrastructure(message),
        Error::Database(error) => MailForwardingHandlerError::Infrastructure(error.to_string()),
    }
}
