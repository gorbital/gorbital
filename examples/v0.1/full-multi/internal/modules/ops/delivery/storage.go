package delivery

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"path"
	"strconv"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"gorbital.dev/modules/openapi"
	"gorbital.dev/modules/storage"

	opsusecase "example.com/acme-api/internal/modules/ops/usecase"
)

// StorageStatusResponse is GET /ops/storage.
type StorageStatusResponse struct {
	Driver    string  `json:"driver" doc:"local, s3, spaces, r2 or minio"`
	Bucket    string  `json:"bucket"`
	Endpoint  string  `json:"endpoint,omitempty" doc:"The service's address, or the directory for local"`
	Region    string  `json:"region,omitempty"`
	Local     bool    `json:"local" doc:"A store on this machine, safe to change freely; the portal warns before writes elsewhere"`
	PublicURL string  `json:"public_url,omitempty" doc:"Where objects are reachable without a signature, when the bucket is public"`
	Status    string  `json:"status" enum:"ok,error" doc:"Whether the service answered"`
	Error     string  `json:"error,omitempty"`
	PingMS    float64 `json:"ping_ms"`
}

// StorageObject is one object.
type StorageObject struct {
	Key          string            `json:"key"`
	Size         int64             `json:"size"`
	ContentType  string            `json:"content_type"`
	ETag         string            `json:"etag,omitempty"`
	LastModified time.Time         `json:"last_modified"`
	Metadata     map[string]string `json:"metadata,omitempty"`
}

// StoragePage is GET /ops/storage/objects.
type StoragePage struct {
	Prefix     string          `json:"prefix"`
	Objects    []StorageObject `json:"objects"`
	Prefixes   []string        `json:"prefixes" doc:"Directories under the prefix (keys folded at the next slash)"`
	NextCursor string          `json:"next_cursor,omitempty"`
}

// SignedURLResponse is POST /ops/storage/signed-url.
type SignedURLResponse struct {
	Key       string    `json:"key"`
	Method    string    `json:"method"`
	URL       string    `json:"url"`
	ExpiresAt time.Time `json:"expires_at"`
}

type storageStatusOutput struct{ Body StorageStatusResponse }
type storageObjectOutput struct{ Body StorageObject }
type storagePageOutput struct{ Body StoragePage }
type signedURLOutput struct{ Body SignedURLResponse }
type storagePrefixOutput struct {
	Body struct {
		Prefix string `json:"prefix"`
	}
}

type listObjectsInput struct {
	Prefix    string `query:"prefix" doc:"List keys under this prefix (a directory ends with a slash)"`
	Recursive bool   `query:"recursive" doc:"Every key under the prefix instead of one level"`
	Cursor    string `query:"cursor"`
	Limit     int    `query:"limit" minimum:"1" maximum:"1000" doc:"Page size, default 200"`
}

type objectKeyInput struct {
	Key string `query:"key" required:"true" minLength:"1" maxLength:"1024" doc:"The object's key"`
}

type putObjectInput struct {
	Key         string `query:"key" required:"true" minLength:"1" maxLength:"1024"`
	ContentType string `header:"Content-Type"`
	RawBody     []byte
}

type moveObjectInput struct {
	Body struct {
		From string `json:"from" minLength:"1" maxLength:"1024"`
		To   string `json:"to" minLength:"1" maxLength:"1024"`
	}
}

type makeDirectoryInput struct {
	Body struct {
		Prefix string `json:"prefix" minLength:"1" maxLength:"1024" doc:"The directory to create, with or without a trailing slash"`
	}
}

type signedURLInput struct {
	Body struct {
		Key           string `json:"key" minLength:"1" maxLength:"1024"`
		Method        string `json:"method,omitempty" enum:"GET,PUT" doc:"GET downloads, PUT uploads; default GET"`
		ExpirySeconds int    `json:"expiry_seconds,omitempty" minimum:"1" maximum:"604800" doc:"How long the URL works; default 3600"`
	}
}

type storageHandler struct{ svc *opsusecase.Service }

// RegisterStorage adds the file storage operations to api (ADR-0075).
func RegisterStorage(api huma.API, svc *opsusecase.Service) {
	h := &storageHandler{svc: svc}
	tags := []string{"Ops: storage"}
	errs := []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound, http.StatusServiceUnavailable}
	huma.Register(api, huma.Operation{
		OperationID: "ops-get-storage", Method: http.MethodGet, Path: "/ops/storage",
		Summary:     "Describe file storage",
		Description: "The driver, bucket and endpoint, and whether the service answers. `local` is the development driver on this machine.",
		Tags:        tags, Security: openapi.Bearer, Errors: errs,
	}, h.status)
	huma.Register(api, huma.Operation{
		OperationID: "ops-list-storage-objects", Method: http.MethodGet, Path: "/ops/storage/objects",
		Summary:     "List objects",
		Description: "One page of objects under a prefix, with the directories folded at the next slash unless `recursive`. Continue with `cursor`.",
		Tags:        tags, Security: openapi.Bearer, Errors: append(errs, http.StatusUnprocessableEntity),
	}, h.list)
	huma.Register(api, huma.Operation{
		OperationID: "ops-get-storage-object", Method: http.MethodGet, Path: "/ops/storage/object",
		Summary:     "Describe an object",
		Description: "Size, content type, ETag, modification time and metadata of the object named by `key`.",
		Tags:        tags, Security: openapi.Bearer, Errors: append(errs, http.StatusUnprocessableEntity),
	}, h.stat)
	huma.Register(api, huma.Operation{
		OperationID: "ops-download-storage-object", Method: http.MethodGet, Path: "/ops/storage/object/content",
		Summary:     "Download an object",
		Description: "The object's bytes with its content type, as an attachment.",
		Tags:        tags, Security: openapi.Bearer, Errors: append(errs, http.StatusUnprocessableEntity),
	}, h.download)
	huma.Register(api, huma.Operation{
		OperationID: "ops-upload-storage-object", Method: http.MethodPut, Path: "/ops/storage/object",
		Summary:     "Upload an object",
		Description: "Stores the request body under `key`, replacing any object there. The `Content-Type` header is the object's; the key's extension decides otherwise. Audited as `storage.object.uploaded`.",
		Tags:        tags, Security: openapi.Bearer, DefaultStatus: http.StatusCreated, Errors: append(errs, http.StatusUnprocessableEntity),
	}, h.upload)
	huma.Register(api, huma.Operation{
		OperationID: "ops-delete-storage-object", Method: http.MethodDelete, Path: "/ops/storage/object",
		Summary:     "Delete an object",
		Description: "Removes the object named by `key`; a missing key is fine. Audited as `storage.object.deleted`.",
		Tags:        tags, Security: openapi.Bearer, DefaultStatus: http.StatusNoContent, Errors: append(errs, http.StatusUnprocessableEntity),
	}, h.remove)
	huma.Register(api, huma.Operation{
		OperationID: "ops-move-storage-object", Method: http.MethodPost, Path: "/ops/storage/object/move",
		Summary:     "Move an object",
		Description: "Copies `from` to `to` and deletes `from`. Audited as `storage.object.moved`.",
		Tags:        tags, Security: openapi.Bearer, Errors: append(errs, http.StatusUnprocessableEntity),
	}, h.move)
	huma.Register(api, huma.Operation{
		OperationID: "ops-create-storage-directory", Method: http.MethodPost, Path: "/ops/storage/directories",
		Summary:     "Create a directory",
		Description: "Object stores have no directories: this stores a hidden `.keep` marker so the prefix appears in listings. Audited as `storage.directory.created`.",
		Tags:        tags, Security: openapi.Bearer, DefaultStatus: http.StatusCreated, Errors: append(errs, http.StatusUnprocessableEntity),
	}, h.makeDirectory)
	huma.Register(api, huma.Operation{
		OperationID: "ops-create-storage-signed-url", Method: http.MethodPost, Path: "/ops/storage/signed-url",
		Summary:     "Create a signed URL",
		Description: "A URL that downloads (GET) or uploads (PUT) the object until it expires, without other credentials. Audited as `storage.signed_url.created`.",
		Tags:        tags, Security: openapi.Bearer, DefaultStatus: http.StatusCreated, Errors: append(errs, http.StatusUnprocessableEntity),
	}, h.signedURL)
}

func object(o storage.Object) StorageObject {
	return StorageObject{Key: o.Key, Size: o.Size, ContentType: o.ContentType, ETag: o.ETag, LastModified: o.LastModified, Metadata: o.Metadata}
}

func (h *storageHandler) status(ctx context.Context, _ *struct{}) (*storageStatusOutput, error) {
	st, err := h.svc.StorageStatus(ctx)
	if err != nil {
		return nil, err
	}
	return &storageStatusOutput{Body: StorageStatusResponse{Driver: st.Driver, Bucket: st.Bucket, Endpoint: st.Endpoint, Region: st.Region, Local: st.Local, PublicURL: st.PublicURL,
		Status: st.Status, Error: st.Error, PingMS: float64(st.PingDuration) / float64(time.Millisecond)}}, nil
}

func (h *storageHandler) list(ctx context.Context, in *listObjectsInput) (*storagePageOutput, error) {
	page, err := h.svc.ListObjects(ctx, storage.ListOptions{Prefix: in.Prefix, Recursive: in.Recursive, Cursor: in.Cursor, Limit: in.Limit})
	if err != nil {
		return nil, err
	}
	out := StoragePage{Prefix: in.Prefix, Objects: make([]StorageObject, 0, len(page.Objects)), Prefixes: page.Prefixes, NextCursor: page.NextCursor}
	for _, o := range page.Objects {
		out.Objects = append(out.Objects, object(o))
	}
	return &storagePageOutput{Body: out}, nil
}

func (h *storageHandler) stat(ctx context.Context, in *objectKeyInput) (*storageObjectOutput, error) {
	o, err := h.svc.StatObject(ctx, in.Key)
	if err != nil {
		return nil, err
	}
	return &storageObjectOutput{Body: object(o)}, nil
}

func (h *storageHandler) download(ctx context.Context, in *objectKeyInput) (*huma.StreamResponse, error) {
	r, o, err := h.svc.OpenObject(ctx, in.Key)
	if err != nil {
		return nil, err
	}
	return &huma.StreamResponse{Body: func(hctx huma.Context) {
		defer r.Close()
		hctx.SetHeader("Content-Type", o.ContentType)
		hctx.SetHeader("Content-Length", strconv.FormatInt(o.Size, 10))
		hctx.SetHeader("Content-Disposition", `attachment; filename="`+path.Base(o.Key)+`"`)
		hctx.SetHeader("X-Content-Type-Options", "nosniff")
		hctx.SetStatus(http.StatusOK)
		_, _ = io.Copy(hctx.BodyWriter(), r)
	}}, nil
}

func (h *storageHandler) upload(ctx context.Context, in *putObjectInput) (*storageObjectOutput, error) {
	o, err := h.svc.PutObject(ctx, in.Key, bytes.NewReader(in.RawBody), int64(len(in.RawBody)), storage.PutOptions{ContentType: in.ContentType})
	if err != nil {
		return nil, err
	}
	return &storageObjectOutput{Body: object(o)}, nil
}

func (h *storageHandler) remove(ctx context.Context, in *objectKeyInput) (*struct{}, error) {
	return nil, h.svc.DeleteObject(ctx, in.Key)
}

func (h *storageHandler) move(ctx context.Context, in *moveObjectInput) (*storageObjectOutput, error) {
	o, err := h.svc.MoveObject(ctx, in.Body.From, in.Body.To)
	if err != nil {
		return nil, err
	}
	return &storageObjectOutput{Body: object(o)}, nil
}

func (h *storageHandler) makeDirectory(ctx context.Context, in *makeDirectoryInput) (*storagePrefixOutput, error) {
	prefix, err := h.svc.MakeDirectory(ctx, in.Body.Prefix)
	if err != nil {
		return nil, err
	}
	out := &storagePrefixOutput{}
	out.Body.Prefix = prefix
	return out, nil
}

func (h *storageHandler) signedURL(ctx context.Context, in *signedURLInput) (*signedURLOutput, error) {
	method := in.Body.Method
	if method == "" {
		method = http.MethodGet
	}
	u, expires, err := h.svc.SignedURL(ctx, in.Body.Key, method, time.Duration(in.Body.ExpirySeconds)*time.Second)
	if err != nil {
		return nil, err
	}
	return &signedURLOutput{Body: SignedURLResponse{Key: in.Body.Key, Method: method, URL: u, ExpiresAt: expires}}, nil
}
