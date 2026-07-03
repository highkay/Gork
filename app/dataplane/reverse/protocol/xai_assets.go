package protocol

import (
	"net/url"
	"path"
	"strings"

	reverseruntime "github.com/dslzl/gork/app/dataplane/reverse/runtime"
)

var (
	AssetsListURL      = reverseruntime.DefaultEndpointTable().Resolve("assets_list")
	AssetsDeleteURL    = reverseruntime.DefaultEndpointTable().Resolve("assets_delete")
	AssetsDownloadBase = reverseruntime.DefaultEndpointTable().Resolve("assets_download")
	AppChatUploadURL   = reverseruntime.DefaultEndpointTable().Resolve("assets_upload")
)

var extensionMIME = map[string]string{
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".png":  "image/png",
	".webp": "image/webp",
	".mp4":  "video/mp4",
	".webm": "video/webm",
}

func AssetDeleteURL(assetID string) string {
	return reverseruntime.GlobalEndpointTable().Resolve("assets_delete") + "/" + url.PathEscape(assetID)
}

func ResolveDownloadURL(filePath string) (string, string, string) {
	parsed, err := url.Parse(filePath)
	if err == nil && parsed.Scheme != "" && parsed.Host != "" {
		origin := parsed.Scheme + "://" + parsed.Host
		return filePath, origin, origin + "/"
	}
	resolvedPath := filePath
	if !strings.HasPrefix(resolvedPath, "/") {
		resolvedPath = "/" + resolvedPath
	}
	base := reverseruntime.GlobalEndpointTable().Resolve("assets_download")
	assetURL := base + resolvedPath
	return assetURL, base, base + "/"
}

func InferContentType(rawURL string) *string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return nil
	}
	if mimeType, ok := extensionMIME[strings.ToLower(path.Ext(parsed.Path))]; ok {
		return &mimeType
	}
	return nil
}

func ResolveAssetReference(fileID, fileURI, userID string) *string {
	if fileURI != "" {
		assetURL, _, _ := ResolveDownloadURL(fileURI)
		return &assetURL
	}
	if fileID != "" && userID != "" {
		assetURL := reverseruntime.GlobalEndpointTable().Resolve("assets_download") + "/users/" + url.PathEscape(userID) + "/" + url.PathEscape(fileID) + "/content"
		return &assetURL
	}
	return nil
}
