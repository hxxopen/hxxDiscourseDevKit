import { mkdirSync, writeFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

const here = dirname(fileURLToPath(import.meta.url))
const keep = resolve(here, '../../internal/webembed/dist/.keep')
mkdirSync(dirname(keep), { recursive: true })
writeFileSync(keep, '')
