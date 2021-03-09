# vault-kv-search [![CircleCI](https://circleci.com/gh/xbglowx/vault-kv-search.svg?style=svg)](https://circleci.com/gh/xbglowx/vault-kv-search)

## Example Usage
* Search values for the substring 'example.com':

  `> vault-kv-search secret/ example.com`

* Search keys for substring 'example.com':

  `> vault-kv-search --search=key secret/ example.com`

* Search keys and values for substring 'example.com':

  `> vault-kv-search --search=value --search=key secret/ example.com`
