// @ts-check

/** @type {jest.InitialOptions} */
const config = require('../../../jest.config.base')

/** @type {jest.InitialOptions} */
module.exports = { ...config, displayName: 'extension-api-classes', rootDir: __dirname }
