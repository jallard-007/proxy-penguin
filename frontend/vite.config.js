import { defineConfig, loadEnv } from 'vite'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'

export default defineConfig(({ mode }) => {
    const env = loadEnv(mode, process.cwd());

    return {
        plugins: [react(), tailwindcss()],
        build: {
            outDir: 'dist',
            emptyOutDir: true,
        },
        server: {
            proxy: {
                '/api': {
                    target: 'http://localhost:8020',
                    changeOrigin: false,
                    secure: false,
                    configure: (proxy) => {
                        proxy.on('proxyReq', (proxyReq, req) => {
                            const host = req.headers.host
                            if (host) {
                                proxyReq.setHeader('Host', "proxy-penguin-dev.jallard.ca")
                                proxyReq.setHeader('X-Forwarded-Host', host)
                            }
                        })
                    }
                },
            },
        }
    }
});
