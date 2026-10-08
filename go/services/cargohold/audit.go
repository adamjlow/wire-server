package cargohold

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
)

type auditMetadata struct {
	ConvID struct {
		ID     string `json:"id"`
		Domain string `json:"domain"`
	} `json:"convId"`
	Filename string `json:"filename"`
	Filetype string `json:"filetype"`
}

func parseAuditMetadata(raw string) *auditMetadata {
	var meta auditMetadata
	if json.Unmarshal([]byte(raw), &meta) != nil {
		decoded, err := url.PathUnescape(raw)
		if err != nil || json.Unmarshal([]byte(decoded), &meta) != nil {
			return nil
		}
	}
	if !validUUID(meta.ConvID.ID) || !validDomain(meta.ConvID.Domain) {
		return nil
	}
	return &meta
}

func requestPrincipal(r *http.Request) (string, string) {
	kind := "user"
	segments := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(segments) > 0 && looksLikeVersion(segments[0]) {
		segments = segments[1:]
	}
	if len(segments) > 0 && (segments[0] == "bot" || segments[0] == "provider") {
		kind = segments[0]
	}
	return kind, r.Header.Get("Z-" + kind)
}

func (s *Server) audit(event, kind, id, domain string, stored map[string]string, pathField, path, ip string) {
	if !s.Config.Settings.AssetAuditLogEnabled {
		return
	}
	fields := []logField{{"audit", "true"}, {"event", event}}
	prefix := "downloader"
	message := "Asset audit log: download"
	if event == "file-upload" {
		prefix = "uploader"
		message = "Asset audit log: upload"
	}
	if id == "" {
		kind = "N/A internal access"
		id = kind
		domain = kind
	}
	fields = append(fields, logField{prefix + ".type", kind}, logField{prefix + ".id", id}, logField{prefix + ".domain", domain})
	cid, cdom, filename, filetype := "N/A", "N/A", "N/A", "N/A"
	if meta := parseAuditMetadata(stored["wire-metadata"]); meta != nil {
		cid = meta.ConvID.ID
		cdom = meta.ConvID.Domain
		filename = meta.Filename
		filetype = meta.Filetype
	}
	if event != "remote-download" {
		fields = append(fields, logField{"conversation.id", cid}, logField{"conversation.domain", cdom}, logField{"file.name", filename}, logField{"file.type", filetype})
	}
	if event == "download-url-creation" {
		message = "Asset audit log: signed URL creation"
		u, _ := url.Parse(path)
		if u != nil {
			fields = append(fields, logField{"download.url.host", u.Hostname()}, logField{"download.url.path", u.Path})
		}
	} else if event == "file-download" {
		if ip == "" {
			ip = "N/A"
		}
		fields = append(fields, logField{"downloader.backend.ip", ip}, logField{pathField, path})
	} else if event == "remote-download" {
		message = "Asset audit log: remote download"
		fields[1] = logField{"event", "file-download"}
		if ip == "" {
			ip = "N/A"
		}
		fields = append(fields, logField{"remote.domain", path}, logField{"remote.backend.ip", ip})
	} else {
		fields = append(fields, logField{pathField, path})
	}
	s.Logger.Event("Info", message, fields...)
}
