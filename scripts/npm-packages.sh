# shellcheck shell=bash
# The published npm packages (packages/<name>, @hydrogencms/<name>) in dependency order: every package comes after
# the @hydrogencms packages it depends on. Sourced by release.sh (which checks the order) and npm-publish.sh.
NPM_ORDER=(client router bridge react admin-sdk hook adapter-test next sveltekit nuxt astro)
