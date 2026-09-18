package usecase

import (
	"context"

	"example.com/plateful/internal/modules/images/domain"
)

// docs:start confirm-upload-stat

// ConfirmUpload marks the organisation orgID's image ready, after checking
// the object the client uploaded.
//
// The app never saw the bytes: they went from the client straight to the
// bucket. Everything it knows about them it learns from one call to
// storage.Store.Stat, and this is the only place the checks a normal upload
// endpoint would do at the door can happen at all.
//
//   - Stat returns storage.ErrNotFound when there is no object: the client
//     never PUT anything, or the signed URL expired first. That is 409
//     image_not_uploaded, not a 404, because the image row is right here.
//   - Object.Size is the first true size the app has seen. The signed PUT
//     could not cap it, so a client that ignores the documented limit
//     succeeds at uploading and fails here, with the bytes already in the
//     bucket. The row stays pending so the client may replace the object
//     and confirm again, and DELETE removes both.
//   - Object.ContentType is what the uploader claimed in its PUT, which the
//     store kept. Nothing in gorbital sniffs the bytes, so this is a claim
//     being re-checked against the allowed list, not proof of what the file
//     is. An app that must be sure would read the object back and inspect
//     its magic bytes itself.
//
// The row is locked for the transaction so two confirmations of one image
// cannot both measure and both write.
func (s *Service) ConfirmUpload(ctx context.Context, orgID, id string) (domain.Image, error) {
	if _, err := memberID(ctx, orgID); err != nil {
		return domain.Image{}, err
	}
	bucket, err := s.bucket()
	if err != nil {
		return domain.Image{}, err
	}
	var confirmed domain.Image
	err = s.store.InTx(ctx, func(tx Store) error {
		image, err := tx.SelectImage(ctx, orgID, id, true)
		if err != nil {
			return err
		}
		object, err := bucket.Stat(ctx, image.StorageKey)
		if err != nil {
			return err
		}
		next, err := image.Confirm(object.Size, object.ContentType, s.limit(ctx), s.clock())
		if err != nil {
			return err
		}
		confirmed, err = tx.UpdateImage(ctx, next)
		return err
	})
	if err != nil {
		return domain.Image{}, storeError("confirm", err)
	}
	s.audit(ctx, ActionConfirmed, confirmed.ID, map[string]any{
		"content_type": confirmed.ContentType,
		"size_bytes":   confirmed.SizeBytes,
	})
	return confirmed, nil
}

// docs:end confirm-upload-stat
