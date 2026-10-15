use mail_parser::{Address, HeaderName, MessageParser, MimeHeaders, decoders::html::html_to_text};

const MAX_DOCUMENT_BYTES: usize = 20 << 20;
const MAX_ATTACHMENT_BYTES: usize = 10 << 20;
const MAX_MIME_PARTS: usize = 256;
const MAX_ATTACHMENTS: usize = 10;

pub struct EmailAttachment {
    pub filename: String,
    pub mime_type: String,
    pub data: Vec<u8>,
}

pub struct ParsedEmail {
    pub from: String,
    pub subject: String,
    pub date: String,
    pub body: String,
    pub has_attachments: bool,
    pub attachments: Vec<EmailAttachment>,
}

pub fn parse_email(data: &[u8]) -> Result<ParsedEmail, &'static str> {
    if data.is_empty() || data.len() > MAX_DOCUMENT_BYTES {
        return Err("invalid_email_size");
    }
    let parser = MessageParser::new()
        .with_minimal_headers()
        .header_text(HeaderName::Subject);
    let Some(message) = parser.parse(data) else {
        if !data.contains(&b'\r')
            && !data.contains(&b'\n')
            && !data.contains(&b':')
            && let Ok(text) = std::str::from_utf8(data)
            && !text.trim().is_empty()
        {
            return Ok(ParsedEmail {
                from: String::new(),
                subject: String::new(),
                date: String::new(),
                body: text.trim().to_owned(),
                has_attachments: false,
                attachments: Vec::new(),
            });
        }
        return Err("invalid_email");
    };
    if message.parts.len() > MAX_MIME_PARTS {
        return Err("email_too_many_parts");
    }

    let mut body_parts = Vec::new();
    let mut html_parts = Vec::new();
    let mut decoded_text_bytes = 0usize;
    for part in message.text_bodies() {
        if part.is_encoding_problem {
            return Err("invalid_email_encoding");
        }
        let text = part.text_contents().ok_or("invalid_email_encoding")?;
        decoded_text_bytes = decoded_text_bytes.saturating_add(text.len());
        if decoded_text_bytes > MAX_DOCUMENT_BYTES {
            return Err("email_text_too_large");
        }
        let text = text.trim();
        if !text.is_empty() {
            body_parts.push(text.to_owned());
        }
    }
    if body_parts.is_empty() {
        for part in message.html_bodies() {
            if part.is_encoding_problem {
                return Err("invalid_email_encoding");
            }
            let html = part.text_contents().ok_or("invalid_email_encoding")?;
            let text = html_to_text(html);
            decoded_text_bytes = decoded_text_bytes.saturating_add(text.len());
            if decoded_text_bytes > MAX_DOCUMENT_BYTES {
                return Err("email_text_too_large");
            }
            let text = text.trim();
            if !text.is_empty() {
                html_parts.push(text.to_owned());
            }
        }
    }

    let has_attachments = message.attachments().next().is_some();
    let mut attachments = Vec::new();
    let mut attachment_bytes = 0usize;
    let mut attachment_index = 0usize;
    for part in message.attachments() {
        attachment_index += 1;
        if attachment_index > MAX_ATTACHMENTS {
            return Err("email_too_many_attachments");
        }
        if part.is_encoding_problem {
            return Err("invalid_email_attachment_encoding");
        }
        let bytes = part.contents();
        if bytes.is_empty() {
            return Err("empty_email_attachment");
        }
        if bytes.len() > MAX_ATTACHMENT_BYTES {
            return Err("email_attachment_too_large");
        }
        attachment_bytes = attachment_bytes.saturating_add(bytes.len());
        if attachment_bytes.saturating_add(decoded_text_bytes) > MAX_DOCUMENT_BYTES {
            return Err("email_decoded_content_too_large");
        }
        let declared_type = part
            .content_type()
            .map(|content_type| {
                format!(
                    "{}/{}",
                    content_type.ctype(),
                    content_type.subtype().unwrap_or("octet-stream")
                )
                .to_ascii_lowercase()
            })
            .unwrap_or_else(|| "application/octet-stream".to_owned());
        let detected_type = detect_attachment_type(bytes);
        let Some(mime_type) = detected_type else {
            if is_supported_attachment_type(&declared_type) {
                return Err("email_attachment_type_mismatch");
            }
            continue;
        };
        if declared_type != "text/plain"
            && declared_type != "application/octet-stream"
            && declared_type != "binary/octet-stream"
            && declared_type != mime_type
        {
            return Err("email_attachment_type_mismatch");
        }
        attachments.push(EmailAttachment {
            filename: safe_attachment_filename(part.attachment_name(), mime_type, attachment_index),
            mime_type: mime_type.to_owned(),
            data: bytes.to_vec(),
        });
    }

    let body = if body_parts.is_empty() {
        html_parts.join("\n\n")
    } else {
        body_parts.join("\n\n")
    };
    if body.is_empty() && !has_attachments {
        return Err("email_has_no_readable_body");
    }
    let from = message
        .from()
        .and_then(Address::first)
        .and_then(|address| address.name())
        .unwrap_or_default()
        .to_owned();
    Ok(ParsedEmail {
        from,
        subject: message.subject().unwrap_or_default().to_owned(),
        date: message
            .date()
            .map(|date| date.to_rfc3339())
            .unwrap_or_default(),
        body,
        has_attachments,
        attachments,
    })
}

fn detect_attachment_type(data: &[u8]) -> Option<&'static str> {
    if data.starts_with(b"%PDF-") {
        Some("application/pdf")
    } else if data.starts_with(&[0xff, 0xd8, 0xff]) {
        Some("image/jpeg")
    } else if data.starts_with(b"\x89PNG\r\n\x1a\n") {
        Some("image/png")
    } else if data.len() >= 12 && data.starts_with(b"RIFF") && &data[8..12] == b"WEBP" {
        Some("image/webp")
    } else {
        None
    }
}

fn is_supported_attachment_type(mime_type: &str) -> bool {
    matches!(
        mime_type,
        "application/pdf" | "image/jpeg" | "image/png" | "image/webp"
    )
}

fn safe_attachment_filename(name: Option<&str>, mime_type: &str, ordinal: usize) -> String {
    let extension = match mime_type {
        "application/pdf" => ".pdf",
        "image/jpeg" => ".jpg",
        "image/png" => ".png",
        "image/webp" => ".webp",
        _ => "",
    };
    let normalized_name = name.unwrap_or_default().replace('\\', "/");
    let basename = normalized_name
        .rsplit('/')
        .next()
        .unwrap_or_default();
    let mut filename = basename
        .chars()
        .filter(|character| !character.is_control())
        .collect::<String>()
        .trim()
        .to_owned();
    if filename.is_empty() || filename == "." {
        filename = format!("attachment-{ordinal:02}{extension}");
    }
    while filename.len() > 255 {
        filename.pop();
    }
    filename
}
