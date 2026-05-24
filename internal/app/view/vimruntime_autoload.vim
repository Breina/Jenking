
" jenking#Receivers returns the list of global names that have scraped members.
" Used by the dot-trigger to decide whether to fire completion.
function! jenking#Receivers() abort
  return s:receivers
endfunction

" Find the receiver token immediately to the left of the cursor, if the char
" just before the cursor is a period. Returns '' otherwise.
function! s:receiver_at_cursor() abort
  let l:line = getline('.')
  let l:col = col('.') - 1
  if l:col <= 0 || l:line[l:col - 1] !=# '.'
    return ''
  endif
  let l:end = l:col - 1
  let l:start = l:end
  while l:start > 0 && l:line[l:start - 1] =~# '\k'
    let l:start -= 1
  endwhile
  return l:line[l:start : l:end - 1]
endfunction

" s:call_at_cursor walks backward from the cursor to detect whether we're
" inside an open call's argument list. Returns the dotted call name
" (e.g. maven.goal or sh) if so, otherwise empty. Scan respects nested
" parens and single-quoted strings — coarse, line-local only.
function! s:call_at_cursor() abort
  let l:line = getline('.')
  let l:col = col('.') - 2
  let l:depth = 0
  let l:in_str = 0
  while l:col >= 0
    let l:c = l:line[l:col]
    if l:in_str
      if l:c ==# "'"
        let l:in_str = 0
      endif
      let l:col -= 1
      continue
    endif
    if l:c ==# "'"
      let l:in_str = 1
    elseif l:c ==# ')'
      let l:depth += 1
    elseif l:c ==# '('
      if l:depth == 0
        " Found an unclosed open-paren. Token to its left is the call name.
        let l:end = l:col
        let l:start = l:end
        while l:start > 0 && (l:line[l:start - 1] =~# '\k' || l:line[l:start - 1] ==# '.')
          let l:start -= 1
        endwhile
        if l:start == l:end
          return ''
        endif
        return l:line[l:start : l:end - 1]
      endif
      let l:depth -= 1
    endif
    let l:col -= 1
  endwhile
  return ''
endfunction

function! jenking#Complete(findstart, base) abort
  if a:findstart
    let l:line = getline('.')
    let l:col = col('.') - 1
    while l:col > 0 && l:line[l:col - 1] =~# '\k'
      let l:col -= 1
    endwhile
    return l:col
  endif
  " Member mode: cursor is just after receiver-dot-prefix.
  let l:receiver = s:receiver_at_cursor()
  if l:receiver !=# '' && has_key(s:members, l:receiver)
    let l:matches = []
    for l:m in s:members[l:receiver]
      if a:base ==# '' || stridx(l:m.word, a:base) == 0
        call add(l:matches, l:m)
      endif
    endfor
    return l:matches
  endif
  " In-call mode: cursor is inside a method's argument list. Surface the
  " declared parameter names (best-effort — relies on the source GDSL
  " listing them; vague Map params yield a single 'argMap' suggestion).
  let l:call = s:call_at_cursor()
  if l:call !=# '' && has_key(s:call_params, l:call)
    let l:matches = []
    for l:name in s:call_params[l:call]
      if a:base ==# '' || stridx(l:name, a:base) == 0
        call add(l:matches, {'word': l:name, 'abbr': l:name, 'menu': l:call . '(' . l:name . ')', 'kind': 'a'})
      endif
    endfor
    if !empty(l:matches)
      return l:matches
    endif
  endif
  " Top-level mode.
  let l:matches = []
  for l:sym in s:symbols
    if a:base ==# '' || stridx(l:sym.word, a:base) == 0
      call add(l:matches, l:sym)
    endif
  endfor
  return l:matches
endfunction

" jenking#DotTrigger is bound to the . key. It returns the dot followed by
" Ctrl-X Ctrl-O when the word preceding the dot is a known receiver, so
" completion fires automatically for maven.<here> etc.
function! jenking#DotTrigger() abort
  let l:line = getline('.')
  let l:col = col('.') - 1
  let l:end = l:col
  let l:start = l:end
  while l:start > 0 && l:line[l:start - 1] =~# '\k'
    let l:start -= 1
  endwhile
  if l:start == l:end
    return '.'
  endif
  let l:word = l:line[l:start : l:end - 1]
  if has_key(s:members, l:word)
    return ".\<C-x>\<C-o>"
  endif
  return '.'
endfunction

function! jenking#Setup() abort
  syntax enable
  setlocal filetype=groovy
  " Explicitly source our syntax overlay. The standard after/syntax/groovy.vim
  " path relies on vim picking up our rtp entry AND firing groovy syntax for
  " the buffer; both can silently fail (rtp ordering quirks, user has
  " 'filetype off', vim with no groovy syntax shipped, …). Sourcing it
  " ourselves here guarantees the jenking* match groups exist on this
  " buffer regardless.
  if exists('g:jenking_runtime_dir')
    let s:overlay = g:jenking_runtime_dir . '/after/syntax/groovy.vim'
    if filereadable(s:overlay)
      execute 'silent! source ' . fnameescape(s:overlay)
    endif
  endif
  setlocal omnifunc=jenking#Complete
  setlocal completeopt=menuone,noinsert,preview
  if has('patch-8.1.1882') || has('nvim')
    try | setlocal completeopt+=popup | catch /./ | endtry
  endif
  " Newline copies the previous line's indentation; smartindent adds an
  " extra level after a line ending in an open brace.
  setlocal autoindent
  setlocal smartindent
  inoremap <buffer> <C-Space> <C-x><C-o>
  inoremap <buffer> <expr> . jenking#DotTrigger()
  inoremap <buffer> <expr> ( jenking#CallTrigger('(')
  inoremap <buffer> <expr> , jenking#CallTrigger(',')
  inoremap <buffer> <expr> <Space> jenking#SpaceTrigger()
  " Quickfix wiring (Slice 4): the TUI writes errors.qf as it validates.
  setlocal errorformat=%f:%l:%c:\ %m,%f:%l:\ %m
endfunction

" jenking#CallTrigger is bound to ( and , inside a buffer. After the typed
" character is inserted, if the cursor now sits inside a call whose name we
" know parameters for, fire omni completion to surface them.
function! jenking#CallTrigger(ch) abort
  return a:ch . "\<C-r>=jenking#MaybeOpenCall()\<CR>"
endfunction

function! jenking#MaybeOpenCall() abort
  let l:call = s:call_at_cursor()
  if l:call !=# '' && has_key(s:call_params, l:call) && !empty(s:call_params[l:call])
    call feedkeys("\<C-x>\<C-o>", 'n')
  endif
  return ''
endfunction

" Space after ( or , inside a known call: reopen the parameter popup. Any
" other context: behave as a literal space.
function! jenking#SpaceTrigger() abort
  let l:line = getline('.')
  let l:col = col('.') - 1
  if l:col > 0
    let l:prev = l:line[l:col - 1]
    if l:prev ==# '(' || l:prev ==# ','
      return " \<C-r>=jenking#MaybeOpenCall()\<CR>"
    endif
  endif
  return ' '
endfunction

" jenking#Find lists every symbol (top-level + receiver-scoped) whose name
" contains the given substring. Use it to confirm what got loaded when omni
" completion comes back empty.
function! jenking#Find(query) abort
  let l:hits = []
  let l:q = a:query
  for l:sym in s:symbols
    if l:q ==# '' || stridx(l:sym.word, l:q) >= 0
      call add(l:hits, printf('%-30s %s', l:sym.word, l:sym.menu))
    endif
  endfor
  for l:recv in s:receivers
    for l:m in s:members[l:recv]
      let l:qualified = l:recv . '.' . l:m.word
      if l:q ==# '' || stridx(l:qualified, l:q) >= 0
        call add(l:hits, printf('%-30s %s', l:qualified, l:m.menu))
      endif
    endfor
  endfor
  if empty(l:hits)
    echohl WarningMsg | echo 'jenking: no symbol matches ' . string(l:q) | echohl None
    return
  endif
  echo join(l:hits, "\n")
endfunction

" :JenkingStatus prints what's loaded — useful when omni completion comes up
" empty and you're not sure why.
command! JenkingStatus echo printf("steps=%d  globals=%d  receivers=%s", s:steps_count, s:globals_count, string(s:receivers))

" :JenkingFind <substring> lists matching symbols (top-level + receiver.member).
" Example: :JenkingFind archive  → see whether 'archive' is loaded.
command! -nargs=? JenkingFind call jenking#Find(<q-args>)
