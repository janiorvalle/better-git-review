package app

import (
	"github.com/janiorvalle/better-git-review/internal/provider"
	claudeprovider "github.com/janiorvalle/better-git-review/internal/provider/claude"
	codexprovider "github.com/janiorvalle/better-git-review/internal/provider/codex"
	mockprovider "github.com/janiorvalle/better-git-review/internal/provider/mock"
	openrouterprovider "github.com/janiorvalle/better-git-review/internal/provider/openrouter"
	"github.com/janiorvalle/better-git-review/internal/source"
	gitsource "github.com/janiorvalle/better-git-review/internal/source/git"
	githubsource "github.com/janiorvalle/better-git-review/internal/source/github"
	patchsource "github.com/janiorvalle/better-git-review/internal/source/patch"
)

func defaultProviderRegistry() provider.Registry {
	return provider.NewRegistry(
		claudeprovider.Adapter{},
		codexprovider.Adapter{},
		openrouterprovider.Adapter{},
		mockprovider.Adapter{},
	)
}

func defaultSourceRegistry() source.Registry {
	return source.NewRegistry(
		githubsource.Source{},
		patchsource.Source{},
		gitsource.Source{},
	)
}
