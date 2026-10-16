"""Read attachments with native record access; document contents are untrusted data."""
import base64
import io
import json
from email import policy
from email.parser import BytesParser
from pathlib import PurePath
from odoo_mcp.security.guards import guard_model_access

MAX_BYTES = 20 * 1024 * 1024
MAX_TEXT = 35000


def parse_document(data, name, mimetype="", embedded_index=None):
    if len(data) > MAX_BYTES:
        raise ValueError("Attachment exceeds the 20 MB reading limit.")
    suffix = PurePath(name).suffix.lower()
    if suffix == ".msg":
        import extract_msg
        with extract_msg.Message(io.BytesIO(data)) as message:
            attachments = [{"index": i, "name": att.longFilename or att.shortFilename or f"attachment_{i}"}
                           for i, att in enumerate(message.attachments)]
            if embedded_index is not None:
                if type(embedded_index) is not int or not 0 <= embedded_index < len(message.attachments):
                    raise ValueError("Invalid embedded attachment index.")
                attachment = message.attachments[embedded_index]
                raw = attachment.data
                if not isinstance(raw, bytes):
                    raise ValueError("This embedded item is not a regular file attachment.")
                return parse_document(raw, attachments[embedded_index]["name"])
            body = message.body or ""
            return {"format": "outlook_msg", "subject": message.subject, "sender": message.sender,
                    "to": message.to, "cc": message.cc, "date": str(message.date),
                    "text": body[:MAX_TEXT], "truncated": len(body) > MAX_TEXT, "attachments": attachments}
    if embedded_index is not None:
        raise ValueError("Embedded attachment selection is supported for .msg files.")
    if suffix == ".eml":
        message = BytesParser(policy=policy.default).parsebytes(data)
        body = message.get_body(preferencelist=("plain", "html"))
        text = body.get_content() if body else ""
        return {"format": "email", "subject": message.get("subject"), "sender": message.get("from"),
                "to": message.get("to"), "text": text[:MAX_TEXT], "truncated": len(text) > MAX_TEXT,
                "attachments": [{"name": part.get_filename()} for part in message.iter_attachments()]}
    if suffix == ".pdf" or mimetype == "application/pdf":
        from pypdf import PdfReader
        reader = PdfReader(io.BytesIO(data))
        total_pages = len(reader.pages)
        page_chunks = []
        for i, page in enumerate(reader.pages):
            txt = page.extract_text() or ""
            lines = [line.strip() for line in txt.splitlines() if line.strip()]
            cleaned = "\n".join(lines)
            if cleaned:
                page_chunks.append(f"--- PÁGINA {i+1}/{total_pages} ---\n{cleaned}")
        text = "\n\n".join(page_chunks)
    elif suffix in {".txt", ".csv", ".json", ".xml", ".html", ".md", ".log"} or mimetype.startswith("text/"):
        text = data.decode("utf-8-sig", errors="replace")
    else:
        raise ValueError("This reader supports MSG, EML, PDF and text files. Use a suitable document tool for other formats.")
    return {"format": suffix.lstrip("."), "text": text[:MAX_TEXT], "truncated": len(text) > MAX_TEXT}


def read_attachment(client, user_id, attachment_id, embedded_index=None):
    guard_model_access("ir.attachment", client, sender_id=user_id)
    records = client.call_kw("ir.attachment", "read", args=[[attachment_id]],
                            kwargs={"fields": ["name", "mimetype", "file_size"]}, sender_id=user_id)
    if not records:
        raise ValueError("Attachment not found.")
    attachment = records[0]
    if attachment.get("file_size", 0) > MAX_BYTES:
        raise ValueError("Attachment exceeds the 20 MB reading limit.")
    raw = client.call_kw("ir.attachment", "read", args=[[attachment_id]], kwargs={"fields": ["datas"]}, sender_id=user_id)[0]
    data = base64.b64decode(raw.get("datas") or "", validate=True)
    result = parse_document(data, attachment["name"], attachment.get("mimetype") or "", embedded_index)
    return {"attachment_id": attachment_id, "name": attachment["name"], "untrusted_document_content": True, **result}
