
function! jenking#WriteBuffer() abort
  silent execute 'write! ' . fnameescape(g:jenking_buffer . '.tmp')
  call rename(g:jenking_buffer . '.tmp', g:jenking_buffer)
  echohl Comment | echom 'jenking: sent for validation…' | echohl None
endfunction

function! jenking#ReloadQF(timer) abort
  if !filereadable(g:jenking_errors) | return | endif
  let l:mtime = getftime(g:jenking_errors)
  if exists('s:qf_mtime') && s:qf_mtime ==# l:mtime | return | endif
  let s:qf_mtime = l:mtime
  execute 'cgetfile ' . fnameescape(g:jenking_errors)
  let l:n = len(getqflist())
  if l:n == 0
    echohl ModeMsg | echom 'jenking: validation OK (declarative checks only)' | echohl None
  else
    echohl WarningMsg | echom printf('jenking: %d validation issue(s) — :cnext to navigate', l:n) | echohl None
  endif
endfunction
