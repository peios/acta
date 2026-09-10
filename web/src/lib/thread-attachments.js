/** Create browser-local URLs for provider-supplied PDFs. Caller owns revocation.
 * @param {unknown} value
 * @returns {{name:string, url:string, size:number}[]}
 */
export function pdfAttachments(value) {
  if (!Array.isArray(value)) return [];
  return value.flatMap((file) => {
    if (
      file?.media_type !== "application/pdf" ||
      typeof file.base64 !== "string" ||
      !file.base64 ||
      typeof file.name !== "string"
    )
      return [];
    try {
      const bytes = Uint8Array.from(atob(file.base64), (char) =>
        char.charCodeAt(0),
      );
      return [
        {
          name: file.name,
          url: URL.createObjectURL(
            new Blob([bytes], { type: "application/pdf" }),
          ),
          size: bytes.length,
        },
      ];
    } catch {
      return [];
    }
  });
}
