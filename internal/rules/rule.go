package rules

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"

	"github.com/dadrus/heimdall/internal/config"
	"github.com/dadrus/heimdall/internal/heimdall"
	"github.com/dadrus/heimdall/internal/pipeline"
	"github.com/dadrus/heimdall/internal/pipeline/interfaces"
)

type Rule interface {
	Id() string
	Execute(ctx context.Context, ads interfaces.AuthDataSource) (*heimdall.SubjectContext, error)
	MatchesUrl(requestUrl *url.URL) bool
	MatchesMethod(method string) bool
}

func createAuthenticator(pr pipeline.Repository, configuredAuthenticators []config.PipelineObjectReference, defaultAuthenticators []config.PipelineObjectReference) (interfaces.Authenticator, error) {
	var refs []config.PipelineObjectReference
	if len(configuredAuthenticators) == 0 {
		if len(defaultAuthenticators) == 0 {
			return nil, errors.New("no default authenticators configured")
		}
		refs = defaultAuthenticators
	} else {
		refs = configuredAuthenticators
	}

	var ans compositeAuthenticator
	for _, ref := range refs {
		a, err := pr.Authenticator(ref.Id)
		if err != nil {
			return nil, err
		}

		if len(ref.Config) != 0 {
			na, err := a.WithConfig(ref.Config)
			if err != nil {
				return nil, err
			}
			ans = append(ans, na)
		} else {
			ans = append(ans, a)
		}
	}

	return ans, nil
}

func createAuthorizer(pr pipeline.Repository, configuredAuthorizer *config.PipelineObjectReference, defaultAuthorizer *config.PipelineObjectReference) (interfaces.Authorizer, error) {
	var ref *config.PipelineObjectReference
	if configuredAuthorizer == nil {
		if defaultAuthorizer == nil {
			return nil, errors.New("no default authorizer configured")
		}
		ref = defaultAuthorizer
	} else {
		ref = configuredAuthorizer
	}

	a, err := pr.Authorizer(ref.Id)
	if err == nil {
		if len(ref.Config) != 0 {
			return a.WithConfig(ref.Config)
		}
		return a, nil
	}
	return a, err
}

func createHydrator(pr pipeline.Repository, configuredHydrators []config.PipelineObjectReference, defaultHydrators []config.PipelineObjectReference) (interfaces.Hydrator, error) {
	var refs []config.PipelineObjectReference
	if len(configuredHydrators) == 0 {
		if len(defaultHydrators) != 0 {
			refs = defaultHydrators
		}
	} else {
		refs = configuredHydrators
	}

	var hs compositeHydrator
	for _, ref := range refs {
		h, err := pr.Hydrator(ref.Id)
		if err != nil {
			return nil, err
		}

		if len(ref.Config) != 0 {
			nh, err := h.WithConfig(ref.Config)
			if err != nil {
				return nil, err
			}
			hs = append(hs, nh)
		} else {
			hs = append(hs, h)
		}
	}

	return hs, nil
}

func createMutator(pr pipeline.Repository, configuredMutators []config.PipelineObjectReference, defaultMutators []config.PipelineObjectReference) (interfaces.Mutator, error) {
	var refs []config.PipelineObjectReference
	if len(configuredMutators) == 0 {
		if len(defaultMutators) == 0 {
			return nil, errors.New("no default mutators configured")
		}
		refs = defaultMutators
	} else {
		refs = configuredMutators
	}

	var ms compositeMutator
	for _, ref := range refs {
		m, err := pr.Mutator(ref.Id)
		if err != nil {
			return nil, err
		}

		if len(ref.Config) != 0 {
			nm, err := m.WithConfig(ref.Config)
			if err != nil {
				return nil, err
			}
			ms = append(ms, nm)
		} else {
			ms = append(ms, m)
		}
	}

	return ms, nil
}

func createErrorHandler(pr pipeline.Repository, configuredErrorHandlers []config.PipelineObjectReference, defaultErrorHandlers []config.PipelineObjectReference) (interfaces.ErrorHandler, error) {
	var refs []config.PipelineObjectReference
	if len(configuredErrorHandlers) == 0 {
		if len(defaultErrorHandlers) == 0 {
			return nil, errors.New("no default error handler configured")
		}
		refs = defaultErrorHandlers
	} else {
		refs = configuredErrorHandlers
	}

	var ehs compositeErrorHandler
	for _, ref := range refs {
		eh, err := pr.ErrorHandler(ref.Id)
		if err != nil {
			return nil, err
		}

		if len(ref.Config) != 0 {
			neh, err := eh.WithConfig(ref.Config)
			if err != nil {
				return nil, err
			}
			ehs = append(ehs, neh)
		} else {
			ehs = append(ehs, eh)
		}
	}

	return ehs, nil
}

func newRule(pr pipeline.Repository, p config.Pipeline, srcId string, rc config.RuleConfig) (*rule, error) {
	an, err := createAuthenticator(pr, rc.Authenticators, p.Authenticators)
	if err != nil {
		return nil, err
	}

	az, err := createAuthorizer(pr, rc.Authorizer, p.Authorizer)
	if err != nil {
		return nil, err
	}

	h, err := createHydrator(pr, rc.Hydrators, p.Hydrators)
	if err != nil {
		return nil, err
	}

	m, err := createMutator(pr, rc.Mutators, p.Mutators)
	if err != nil {
		return nil, err
	}

	eh, err := createErrorHandler(pr, rc.ErrorHandlers, p.ErrorHandlers)
	if err != nil {
		return nil, err
	}

	return &rule{
		id:    rc.Id,
		url:   rc.Url,
		srcId: srcId,
		an:    an,
		az:    az,
		h:     h,
		m:     m,
		eh:    eh,
	}, nil
}

type rule struct {
	id    string
	url   string
	srcId string
	an    interfaces.Authenticator
	az    interfaces.Authorizer
	h     interfaces.Hydrator
	m     interfaces.Mutator
	eh    interfaces.ErrorHandler
}

func (r *rule) Execute(ctx context.Context, ads interfaces.AuthDataSource) (*heimdall.SubjectContext, error) {
	sc := &heimdall.SubjectContext{}

	if err := r.an.Authenticate(ctx, ads, sc); err != nil {
		return nil, r.eh.HandleError(ctx, err)
	}

	if err := r.az.Authorize(ctx, sc); err != nil {
		return nil, r.eh.HandleError(ctx, err)
	}

	if err := r.h.Hydrate(ctx, sc); err != nil {
		return nil, r.eh.HandleError(ctx, err)
	}

	if err := r.m.Mutate(ctx, sc); err != nil {
		return nil, r.eh.HandleError(ctx, err)
	}

	return sc, nil
}

func (r *rule) MatchesUrl(requestUrl *url.URL) bool {
	return true
}

func (r *rule) MatchesMethod(method string) bool {
	return true
}

func (r *rule) Id() string {
	return r.id
}

type compositeAuthenticator []interfaces.Authenticator

func (ca compositeAuthenticator) Authenticate(c context.Context, ads interfaces.AuthDataSource, sc *heimdall.SubjectContext) error {
	var err error
	for _, a := range ca {
		err = a.Authenticate(c, ads, sc)
		if err != nil {
			// try next
			continue
		} else {
			return nil
		}
	}
	return err
}

func (ca compositeAuthenticator) WithConfig(_ json.RawMessage) (interfaces.Authenticator, error) {
	return nil, errors.New("reconfiguration not allowed")
}

type compositeHydrator []interfaces.Hydrator

func (ch compositeHydrator) Hydrate(c context.Context, sc *heimdall.SubjectContext) error {
	var err error
	for _, h := range ch {
		err = h.Hydrate(c, sc)
		if err != nil {
			// try next
			continue
		} else {
			return nil
		}
	}
	return err
}

func (ch compositeHydrator) WithConfig(_ json.RawMessage) (interfaces.Hydrator, error) {
	return nil, errors.New("reconfiguration not allowed")
}

type compositeMutator []interfaces.Mutator

func (cm compositeMutator) Mutate(c context.Context, sc *heimdall.SubjectContext) error {
	var err error
	for _, m := range cm {
		err = m.Mutate(c, sc)
		if err != nil {
			// try next
			continue
		} else {
			return nil
		}
	}
	return err
}

func (cm compositeMutator) WithConfig(_ json.RawMessage) (interfaces.Mutator, error) {
	return nil, errors.New("reconfiguration not allowed")
}

type compositeErrorHandler []interfaces.ErrorHandler

func (ceh compositeErrorHandler) HandleError(ctx context.Context, e error) error {
	var err error
	for _, eh := range ceh {
		err = eh.HandleError(ctx, e)
		if err != nil {
			// try next
			continue
		} else {
			return nil
		}
	}
	return err
}

func (compositeErrorHandler) WithConfig(_ json.RawMessage) (interfaces.ErrorHandler, error) {
	return nil, errors.New("reconfiguration not allowed")
}
