package controller

import (
	"context"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// createOrUpdateWithRetries attempts to create or update an object with exponential backoff retry logic
func (r *DHCPServerReconciler) createOrUpdateWithRetries(ctx context.Context, obj client.Object, updateFunc func() error) error {
	logger := log.FromContext(ctx)
	key := client.ObjectKeyFromObject(obj)

	// Use exponential backoff for retries
	backoff := wait.Backoff{
		Steps:    retry.DefaultBackoff.Steps,
		Duration: retry.DefaultBackoff.Duration,
		Factor:   retry.DefaultBackoff.Factor,
		Jitter:   retry.DefaultBackoff.Jitter,
	}

	err := wait.ExponentialBackoffWithContext(ctx, backoff, func(ctx context.Context) (bool, error) {
		// Try to get the object
		if err := r.Get(ctx, key, obj); err != nil {
			if errors.IsNotFound(err) {
				// Object doesn't exist, create it
				logger.Info("Creating object", "kind", obj.GetObjectKind().GroupVersionKind().Kind, "name", key.Name)
				if createErr := r.Create(ctx, obj); createErr != nil {
					if errors.IsAlreadyExists(createErr) {
						// Race condition: object was created between Get and Create
						logger.V(1).Info("Object already exists, will update", "kind", obj.GetObjectKind().GroupVersionKind().Kind, "name", key.Name)
						return false, nil // Retry
					}
					logger.Error(createErr, "Failed to create object", "kind", obj.GetObjectKind().GroupVersionKind().Kind, "name", key.Name)
					return false, createErr
				}
				return true, nil // Success
			}
			// Other error
			logger.Error(err, "Failed to get object", "kind", obj.GetObjectKind().GroupVersionKind().Kind, "name", key.Name)
			return false, err
		}

		// Object exists, update it
		logger.V(1).Info("Updating object", "kind", obj.GetObjectKind().GroupVersionKind().Kind, "name", key.Name)
		if updateErr := updateFunc(); updateErr != nil {
			logger.Error(updateErr, "Update function failed", "kind", obj.GetObjectKind().GroupVersionKind().Kind, "name", key.Name)
			return false, updateErr
		}

		if updateErr := r.Update(ctx, obj); updateErr != nil {
			if errors.IsConflict(updateErr) {
				// Conflict: object was modified, retry
				logger.V(1).Info("Conflict updating object, retrying", "kind", obj.GetObjectKind().GroupVersionKind().Kind, "name", key.Name)
				return false, nil // Retry
			}
			logger.Error(updateErr, "Failed to update object", "kind", obj.GetObjectKind().GroupVersionKind().Kind, "name", key.Name)
			return false, updateErr
		}

		return true, nil // Success
	})

	if err != nil {
		return err
	}

	return nil
}
