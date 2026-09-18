package usecase

import "context"

// DeleteImage removes one of the organisation orgID's images: the object in
// the bucket first, then the row.
//
// That order is deliberate. The row is the only record of the key, so
// deleting it first would leave an object nothing can name and nothing will
// ever collect. Deleting the object first can leave a row whose object is
// gone, which every other operation already handles: confirming answers
// image_not_uploaded and the signed URL answers 404, and a second DELETE
// clears it. storage.Store.Delete treats a missing key as success, so the
// retry is safe.
func (s *Service) DeleteImage(ctx context.Context, orgID, id string) error {
	if _, err := memberID(ctx, orgID); err != nil {
		return err
	}
	bucket, err := s.bucket()
	if err != nil {
		return err
	}
	image, err := s.store.SelectImage(ctx, orgID, id, false)
	if err != nil {
		return storeError("delete", err)
	}
	if err := bucket.Delete(ctx, image.StorageKey); err != nil {
		return storeError("delete object", err)
	}
	if err := s.store.DeleteImage(ctx, orgID, id); err != nil {
		return storeError("delete", err)
	}
	s.audit(ctx, ActionDeleted, id, map[string]any{"purpose": string(image.Purpose)})
	return nil
}
