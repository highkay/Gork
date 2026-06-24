export function fileToAttachment(file) {
  return {
    name: file?.name || 'attachment',
    type: file?.type || 'application/octet-stream',
    size: Number(file?.size || 0),
  };
}
