import hydrogen from 'virtual:hydrogen/instance'
import env from 'virtual:hydrogen/env'

hydrogen.configure(env)

export const onRequest = hydrogen.middleware
