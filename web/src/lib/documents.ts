export type DocumentVersion = {
    file_id: string;
    document_id: string;
    revision: number;
    title: string;
    filename: string;
    media_type: string;
    size: number;
    sha256: string;
    created_by: string;
    created_at: string;
};
export type TaskDocument = DocumentVersion & {
    id: string;
    task_id: string;
    can_write: boolean;
};
export type DocumentPage = { documents: TaskDocument[]; cursor?: string };
export type DocumentHistory = { versions: DocumentVersion[]; before?: number };
export const DOCUMENT_LIMIT_BYTES = 20 * 1024 * 1024;
export function documentSize(sizeBytes: number) {
    if (sizeBytes < 1024) return `${sizeBytes} B`;
    if (sizeBytes < 1024 * 1024) return `${(sizeBytes / 1024).toFixed(1)} KB`;
    return `${(sizeBytes / (1024 * 1024)).toFixed(1)} MB`;
}
export function documentPreview(type: string, sizeBytes: number) {
    const mime = type.split(";")[0];
    if (["image/png", "image/jpeg", "image/gif", "image/webp"].includes(mime)) return "image";
    if (mime === "application/pdf") return "pdf";
    if (sizeBytes <= 1024 * 1024 && mime === "text/markdown") return "markdown";
    if (sizeBytes <= 1024 * 1024 && mime === "text/plain") return "text";
    return "download";
}
export function documentURL(version: DocumentVersion) {
    return `/api/documents/${encodeURIComponent(version.document_id)}/versions/${version.revision}/file`;
}
