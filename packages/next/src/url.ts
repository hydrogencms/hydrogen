/**
 * The request path, set by the proxy (which first deletes what the client sent) for the navigation, whose layout
 * cannot see the path. It only selects among the configured channels for that one response; pages never use it.
 */
export const PATH_HEADER = 'x-hydrogen-path'
