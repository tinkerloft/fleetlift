import YAML from 'js-yaml'

export function parse(yamlStr: string): Record<string, unknown> {
  const result = YAML.load(yamlStr)
  if (typeof result !== 'object' || result === null) {
    return {}
  }
  return result as Record<string, unknown>
}
