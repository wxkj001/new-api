/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { useCallback, useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { IconWeChat } from '@/assets/brand-icons'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { cn } from '@/lib/utils'

import { checkWeChatMpStatus, generateWeChatMpUrl } from '../api'
import { useAuthRedirect } from '../hooks/use-auth-redirect'
import type { WeChatMpStatus } from '../api'

const POLLING_INTERVAL_MS = 2000
const POLLING_TIMEOUT_MS = 5 * 60 * 1000

interface WeChatMpLoginDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
}

export function WeChatMpLoginDialog({
  open,
  onOpenChange,
}: WeChatMpLoginDialogProps) {
  const { t } = useTranslation()
  const { handleLoginSuccess } = useAuthRedirect()

  const [status, setStatus] = useState<WeChatMpStatus>('pending')
  const [code, setCode] = useState('')
  const [qrImage, setQrImage] = useState('')
  const [errorMessage, setErrorMessage] = useState('')

  const pollingRef = useRef<ReturnType<typeof setInterval> | null>(null)
  const timeoutRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  const initializedRef = useRef(false)

  const stopPolling = useCallback(() => {
    if (pollingRef.current) {
      clearInterval(pollingRef.current)
      pollingRef.current = null
    }
    if (timeoutRef.current) {
      clearTimeout(timeoutRef.current)
      timeoutRef.current = null
    }
  }, [])

  const handleClose = useCallback(
    (isOpen: boolean) => {
      if (!isOpen) {
        stopPolling()
        initializedRef.current = false
      }
      onOpenChange(isOpen)
    },
    [onOpenChange, stopPolling]
  )

  const initFlow = useCallback(async () => {
    try {
      setStatus('pending')
      setErrorMessage('')
      const res = await generateWeChatMpUrl()
      if (res.success && res.data) {
        setCode(res.data.code)
        setQrImage(res.data.qr_image)
      } else {
        setStatus('failed')
        setErrorMessage(
          res.message || t('WeChat Mini Program authorization failed')
        )
      }
    } catch {
      setStatus('failed')
      setErrorMessage(t('WeChat Mini Program authorization failed'))
      toast.error(t('WeChat Mini Program authorization failed'))
    }
  }, [t])

  const handleRetry = useCallback(() => {
    stopPolling()
    initializedRef.current = false
    setCode('')
    setQrImage('')
    void initFlow()
  }, [initFlow, stopPolling])

  useEffect(() => {
    if (open && !initializedRef.current) {
      initializedRef.current = true
      void initFlow()
    }
    return () => stopPolling()
  }, [open, initFlow, stopPolling])

  useEffect(() => {
    if (!code || status !== 'pending') return

    let pollCount = 0
    const maxPolls = POLLING_TIMEOUT_MS / POLLING_INTERVAL_MS

    pollingRef.current = setInterval(() => {
      pollCount++
      if (pollCount > maxPolls) {
        stopPolling()
        setStatus('expired')
        setErrorMessage(t('Login code expired, please try again'))
        return
      }

	      void checkWeChatMpStatus(code).then((res) => {
	        if (res.status === 'success' || (res.success && res.data)) {
	          stopPolling()
	          handleClose(false)
	          handleLoginSuccess(
	            res.data as { id?: number } | null,
	            '/'
	          )
	        } else if (res.status === 'failed') {
          stopPolling()
          setStatus('failed')
          setErrorMessage(
            res.message || t('WeChat Mini Program authorization failed')
          )
        } else if (res.status === 'expired') {
          stopPolling()
          setStatus('expired')
          setErrorMessage(
            res.message || t('Login code expired, please try again')
          )
        }
      }).catch(() => {})
    }, POLLING_INTERVAL_MS)

    return () => stopPolling()
  }, [code, status, handleClose, t, stopPolling])

  const isTerminal = status === 'failed' || status === 'expired'

  return (
    <Dialog open={open} onOpenChange={handleClose}>
      <DialogContent className='sm:max-w-md'>
        <DialogHeader>
          <DialogTitle className='flex items-center gap-2'>
            <IconWeChat className='h-5 w-5 text-[#07C160]' />
            {t('WeChat Mini Program Login')}
          </DialogTitle>
          <DialogDescription>
            {t('Scan the QR code with WeChat to login via Mini Program')}
          </DialogDescription>
        </DialogHeader>

        <div className='flex flex-col gap-4 py-2'>
          {status === 'pending' && !qrImage && (
            <div className='flex flex-col items-center gap-3 py-4'>
              <svg
                className='text-muted-foreground h-8 w-8 animate-spin'
                xmlns='http://www.w3.org/2000/svg'
                fill='none'
                viewBox='0 0 24 24'
                aria-hidden='true'
              >
                <circle
                  className='opacity-25'
                  cx='12' cy='12' r='10'
                  stroke='currentColor' strokeWidth='4'
                />
                <path
                  className='opacity-75'
                  fill='currentColor'
                  d='M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4z'
                />
              </svg>
              <p className='text-muted-foreground text-sm'>
                {t('Generating QR code...')}
              </p>
            </div>
          )}

          {isTerminal && (
            <div
              className={cn(
                'flex flex-col items-center gap-3 rounded-lg border px-4 py-4',
                status === 'expired'
                  ? 'border-amber-200 bg-amber-50 text-amber-800 dark:border-amber-800 dark:bg-amber-950 dark:text-amber-200'
                  : 'border-destructive/30 bg-destructive/5 text-destructive'
              )}
            >
              <p className='text-center text-sm font-medium'>{errorMessage}</p>
            </div>
          )}

          {qrImage && (
            <div className='flex flex-col items-center gap-3'>
              <div className='rounded-xl border bg-white p-4'>
                <img
                  src={qrImage}
                  alt='WeChat Mini Program QR Code'
                  className='h-[200px] w-[200px]'
                />
              </div>
              <p className='text-muted-foreground text-sm'>
                {t('Scan with WeChat to login via Mini Program')}
              </p>
              {status === 'pending' && (
                <svg
                  className='text-muted-foreground h-5 w-5 animate-spin'
                  xmlns='http://www.w3.org/2000/svg'
                  fill='none'
                  viewBox='0 0 24 24'
                  aria-hidden='true'
                >
                  <circle className='opacity-25' cx='12' cy='12' r='10' stroke='currentColor' strokeWidth='4' />
                  <path className='opacity-75' fill='currentColor' d='M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4z' />
                </svg>
              )}
            </div>
          )}
        </div>

        <DialogFooter>
          {isTerminal && (
            <Button
              type='button'
              variant='default'
              onClick={handleRetry}
              className='w-full gap-2 sm:w-auto'
            >
              {t('Retry')}
            </Button>
          )}
          <Button
            type='button'
            variant='outline'
            onClick={() => handleClose(false)}
          >
            {t('Cancel')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
