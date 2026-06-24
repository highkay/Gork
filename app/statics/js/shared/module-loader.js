export function currentAssetVersion(importMetaUrl) {
  try {
    return (
      new URL(importMetaUrl, window.location.href).searchParams.get('v') || 'v1'
    );
  } catch {
    return 'v1';
  }
}

export function loadClassicScript(src) {
  return new Promise((resolve, reject) => {
    const script = document.createElement('script');
    script.src = src;
    script.async = false;
    script.onload = () => resolve(script);
    script.onerror = () => reject(new Error(`Failed to load ${src}`));
    document.head.appendChild(script);
  });
}

export function versionedScript(path, importMetaUrl) {
  const version = currentAssetVersion(importMetaUrl);
  return `${path}?v=${encodeURIComponent(version)}`;
}
