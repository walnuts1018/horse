use std::{future::Future, pin::Pin};

use crate::mail_forwarding::{Command, ForwardingAddress, IssueCommand, Status};

#[derive(Debug)]
pub enum MailForwardingHandlerError {
    InvalidInput,
    NotFound,
    Conflict,
    Infrastructure(String),
}

pub trait MailForwardingHandler: Send + Sync {
    fn active<'a>(
        &'a self,
        command: Command,
    ) -> Pin<Box<dyn Future<Output = Result<Status, MailForwardingHandlerError>> + Send + 'a>>;

    fn issue<'a>(
        &'a self,
        command: IssueCommand,
    ) -> Pin<
        Box<dyn Future<Output = Result<ForwardingAddress, MailForwardingHandlerError>> + Send + 'a>,
    >;

    fn rotate<'a>(
        &'a self,
        command: IssueCommand,
    ) -> Pin<
        Box<dyn Future<Output = Result<ForwardingAddress, MailForwardingHandlerError>> + Send + 'a>,
    >;

    fn revoke<'a>(
        &'a self,
        command: Command,
    ) -> Pin<Box<dyn Future<Output = Result<Status, MailForwardingHandlerError>> + Send + 'a>>;
}
