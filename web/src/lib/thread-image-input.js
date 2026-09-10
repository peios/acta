/** @typedef {{id:string,name:string,size:number,media_type:string}} DraftImage */
export const MAX_IMAGE_BYTES = 4 * 1024 * 1024;
export const MAX_IMAGES = 4;
export const IMAGE_TYPES = [
  "image/png",
  "image/jpeg",
  "image/gif",
  "image/webp",
];
/** @param {Array<{size:number,type:string}>} files @param {DraftImage[]} existing */
export function validateImageFiles(files, existing = []) {
  if (files.length + existing.length > MAX_IMAGES)
    throw Error("Attach up to four images.");
  if (files.some((file) => !IMAGE_TYPES.includes(file.type)))
    throw Error("Choose PNG, JPEG, GIF or WebP images.");
  if (
    files.some((file) => !file.size) ||
    [...files, ...existing].reduce((sum, file) => sum + file.size, 0) >
      MAX_IMAGE_BYTES
  )
    throw Error("Images can total up to 4 MiB.");
}
/** @returns {Promise<IDBDatabase>} */
function database() {
  return new Promise((resolve, reject) => {
    const request = indexedDB.open("acta2-image-drafts", 1);
    request.onupgradeneeded = () => request.result.createObjectStore("images");
    request.onsuccess = () => resolve(request.result);
    request.onerror = () =>
      reject(Error("Could not open image draft storage."));
  });
}
/** @param {'readonly'|'readwrite'} mode @param {(store:IDBObjectStore)=>IDBRequest} operation @returns {Promise<any>} */
async function stored(mode, operation) {
  const db = await database();
  try {
    return await new Promise((resolve, reject) => {
      const transaction = db.transaction("images", mode);
      const request = operation(transaction.objectStore("images"));
      transaction.oncomplete = () => resolve(request.result);
      transaction.onabort = transaction.onerror = () =>
        reject(Error("Could not save the image draft in this browser."));
    });
  } finally {
    db.close();
  }
}
/** @param {File} file @returns {Promise<DraftImage>} */
export async function saveImage(file) {
  validateImageFiles([file]);
  const base64 = await new Promise((resolve, reject) => {
    const reader = new FileReader();
    reader.onload = () => resolve(String(reader.result).split(",")[1]);
    reader.onerror = () => reject(Error("Could not read the image."));
    reader.readAsDataURL(file);
  });
  const image = {
    id: crypto.randomUUID(),
    name: file.name.slice(0, 255) || "Image",
    size: file.size,
    media_type: file.type,
  };
  await stored("readwrite", (store) =>
    store.put({ ...image, base64 }, image.id),
  );
  return image;
}
/** @param {DraftImage} ref */
export async function loadImage(ref) {
  const image = await stored("readonly", (store) => store.get(ref.id));
  if (!image || image.media_type !== ref.media_type || image.size !== ref.size)
    throw Error(
      "An attached image is no longer stored in this browser. Remove it and attach it again.",
    );
  return {
    name: image.name,
    media_type: image.media_type,
    base64: image.base64,
  };
}
/** @param {DraftImage} ref */
export async function removeImage(ref) {
  await stored("readwrite", (store) => store.delete(ref.id));
}
/** @param {unknown} refs @returns {DraftImage[]} */
export function imageRefs(refs) {
  if (!Array.isArray(refs)) return [];
  return refs.filter(
    (r) =>
      r &&
      typeof r.id === "string" &&
      typeof r.name === "string" &&
      Number.isFinite(r.size) &&
      r.size > 0 &&
      IMAGE_TYPES.includes(r.media_type),
  );
}
