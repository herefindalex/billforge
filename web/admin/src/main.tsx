import React from 'react'
import ReactDOM from 'react-dom/client'
import { BrowserRouter } from 'react-router-dom'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { App as AntApp, ConfigProvider } from 'antd'
import zhTW from 'antd/locale/zh_TW'
import App from './App'
import './styles.css'

const queryClient = new QueryClient({
  defaultOptions: {
    queries: { retry: false, refetchOnWindowFocus: false, staleTime: 10_000 },
  },
})

ReactDOM.createRoot(document.getElementById('root')!).render(
  <React.StrictMode>
    <QueryClientProvider client={queryClient}>
      <ConfigProvider
        locale={zhTW}
        theme={{
          token: {
            colorPrimary: '#205b63',
            colorInfo: '#205b63',
            borderRadius: 8,
            fontFamily: 'Inter, Noto Sans TC, system-ui, sans-serif',
          },
          components: { Layout: { bodyBg: '#f4f6f5', siderBg: '#122b31' } },
        }}
      >
        <AntApp>
          <BrowserRouter basename="/admin">
            <App />
          </BrowserRouter>
        </AntApp>
      </ConfigProvider>
    </QueryClientProvider>
  </React.StrictMode>,
)
