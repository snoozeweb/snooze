const path = require('path');
module.exports = {
  lintOnSave: false,
  runtimeCompiler: true,
  publicPath: '/web',
  devServer: {
    port: 8082
  },
  chainWebpack: config => {
    config.module
      .rule('supportChaining')
      .test(/\.js$/)
        .include
          .add(path.resolve('node_modules/@coreui'))
          .end()
      .use('babel-loader')
        .loader('babel-loader')
        .tap(options => ({ ...options,
          plugins : ['@babel/plugin-proposal-optional-chaining']
        }))
        .end()
    }
}
