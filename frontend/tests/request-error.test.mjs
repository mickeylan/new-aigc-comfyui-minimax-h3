import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { requestErrorMessage } from '../src/utils/requestError.js'

test('请求失败提示始终给出原因：服务端消息、状态码、超时或网络错误', () => {
  // 服务端 JSON 消息优先，但状态码仍然保留：409 和 500 的处理方式不同。
  assert.equal(
    requestErrorMessage({ response: { status: 409, statusText: 'Conflict', data: { error: '四视图正在生成' } } }, '上传失败'),
    '四视图正在生成（HTTP 409 Conflict）'
  )

  // 反向代理按请求体大小拦截时返回 HTML 错误页：过去这种情况只剩一句「上传失败」，
  // 而 413 正是唯一线索。
  assert.equal(
    requestErrorMessage({ response: { status: 413, statusText: 'Request Entity Too Large', data: '<html><body>413</body></html>' } }, '上传失败'),
    '上传失败：文件超过服务器允许的请求体大小（HTTP 413 Request Entity Too Large）'
  )

  // 网关错误同样没有 JSON 正文，至少要把状态码交给用户。
  assert.equal(
    requestErrorMessage({ response: { status: 502, statusText: 'Bad Gateway', data: '' } }, '上传失败'),
    '上传失败：HTTP 502 Bad Gateway'
  )

  // 纯文本正文（例如 gin 的 404 page not found）可读，保留。
  assert.equal(
    requestErrorMessage({ response: { status: 404, statusText: '', data: '404 page not found' } }, '上传失败'),
    '404 page not found（HTTP 404）'
  )

  // 超时与连不上后端时根本没有 response，e.message 是唯一线索。
  assert.equal(
    requestErrorMessage({ code: 'ECONNABORTED', message: 'timeout of 120000ms exceeded' }, '上传失败'),
    '上传失败：请求超时，可能文件过大或网络过慢'
  )
  assert.equal(
    requestErrorMessage({ message: 'Network Error' }, '上传失败'),
    '上传失败：Network Error'
  )
  assert.equal(requestErrorMessage({}, '上传失败'), '上传失败：无法连接后端服务')
  assert.equal(requestErrorMessage(null, '上传失败'), '上传失败')
})

test('上传失败使用常驻错误Toast并按入口区分原因', () => {
  const source = readFileSync(new URL('../src/views/ProjectDetail.vue', import.meta.url), 'utf8')
  // 旧写法丢弃 e.message，且 toast.show 是 3.4 秒自动消失的信息条，读不完就没了。
  assert.doesNotMatch(source, /toast\.show\(e\.response\?\.data\?\.error \|\| '上传失败'\)/)
  for (const label of ['上传标准像失败', '上传角色四视图失败', '上传参考语音失败', '上传参考图失败', '上传道具四视图失败']) {
    assert.match(source, new RegExp(`toast\\.error\\(requestErrorMessage\\(e, '${label}'\\)\\)`))
  }
})
